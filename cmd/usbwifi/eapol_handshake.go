package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

// runEapolHandshake performs the WPA2-PSK 4-way handshake as a host
// supplicant (the firmware's controlled port is host-driven). It must be
// called right after SM_CONNECT_IND status=0, in the same session.
//
// EAPOL frames arrive via eapolCh as raw 802.1X bytes (extracted from the
// data path by the OnDataFrame hook); replies go out as TX data records on
// the bulk data pipe. On success it installs the PTK/GTK via MM_KEY_ADD and
// opens the controlled port, returning 0.
func runEapolHandshake(ctx context.Context, s *session, vif, apIdx uint8, bssid, staMAC [6]byte, ssid, passphrase string, eapolCh <-chan []byte) int {
	fmt.Printf("  EAPOL: vif=%d apidx=%d sta=%02x:%02x:%02x:%02x:%02x:%02x\n",
		vif, apIdx, staMAC[0], staMAC[1], staMAC[2], staMAC[3], staMAC[4], staMAC[5])
	pmk := lmac.PMK(passphrase, ssid)
	fmt.Println("  EAPOL: PMK derived, waiting up to 30s for msg1 ...")

	// --- msg1 ---
	var msg1 *lmac.KeyFrame
	deadline := time.After(30 * time.Second)
waitMsg1:
	for {
		select {
		case f := <-eapolCh:
			var k lmac.KeyFrame
			if err := k.Decode(f); err != nil {
				fmt.Printf("  EAPOL: non-key frame (%d bytes): %v\n", len(f), err)
				continue
			}
			fmt.Printf("  EAPOL: key frame msg=%d pairwise=%v replay=%d keylen=%d keydatalen=%d\n",
				k.Msg(), k.Pairwise(), k.Replay, k.KeyLen, len(k.KeyData))
			if k.Pairwise() && k.Msg() == 1 {
				msg1 = &k
				break waitMsg1
			}
		case <-deadline:
			fmt.Println("  EAPOL: no msg1 within 30s")
			return 1
		case <-ctx.Done():
			return 1
		}
	}

	// --- msg2 ---
	var sNonce [32]byte
	if _, err := rand.Read(sNonce[:]); err != nil {
		log.Printf("rand SNonce: %v", err)
		return 1
	}
	ptk := lmac.PTK(pmk, bssid[:], staMAC[:], msg1.Nonce[:], sNonce[:])
	kck := ptk[:16]
	kek := ptk[16:32]
	fmt.Printf("  EAPOL: PTK derived (replay %d), sending msg2 ...\n", msg1.Replay)

	msg2 := &lmac.KeyFrame{
		KeyInfo: lmac.KeyInfoVerHMACSHA1 | lmac.KeyInfoPairwise | lmac.KeyInfoMIC,
		KeyLen:  0, // M2 carries no key, only the RSN IE
		Replay:  msg1.Replay,
		KeyData: append([]byte(nil), lmac.WPA2PSKCCMPRsnIE...),
	}
	copy(msg2.Nonce[:], sNonce[:])
	sendEapol := func(k *lmac.KeyFrame) error {
		enc := k.Encode()
		for i := 73; i < 89 && i < len(enc); i++ {
			enc[i] = 0
		}
		mic := lmac.ComputeMIC(kck, enc)
		copy(k.MIC[:], mic)
		tx := &lmac.TxData{
			DA:        bssid,
			SA:        staMAC,
			Ethertype: lmac.EAPOLEthertype,
			VifIdx:    vif,
			StaIdx:    apIdx,
			Payload:   k.Encode(),
		}
		frame, err := tx.Encode()
		if err != nil {
			return err
		}
		return s.sess.BulkOutData(ctx, frame)
	}
	if err := sendEapol(msg2); err != nil {
		log.Printf("send msg2: %v", err)
		return 1
	}

	// --- msg3 (with msg1-retransmit handling) ---
	fmt.Println("  EAPOL: msg2 sent, waiting up to 20s for msg3 ...")
	deadline = time.After(20 * time.Second)
	var msg3 *lmac.KeyFrame
	for {
		select {
		case f := <-eapolCh:
			var k lmac.KeyFrame
			if err := k.Decode(f); err != nil {
				fmt.Printf("  EAPOL: non-key frame (%d bytes): %v\n", len(f), err)
				continue
			}
			fmt.Printf("  EAPOL: key frame msg=%d pairwise=%v replay=%d keylen=%d keydatalen=%d\n",
				k.Msg(), k.Pairwise(), k.Replay, k.KeyLen, len(k.KeyData))
			switch {
			case k.Pairwise() && k.Msg() == 1:
				// AP retransmitted msg1 (didn't get our msg2) — resend.
				fmt.Println("  EAPOL: msg1 retransmit, resending msg2 ...")
				msg1 = &k
				ptk = lmac.PTK(pmk, bssid[:], staMAC[:], msg1.Nonce[:], sNonce[:])
				kck = ptk[:16]
				kek = ptk[16:32]
				msg2.Replay = msg1.Replay
				if err := sendEapol(msg2); err != nil {
					log.Printf("resend msg2: %v", err)
					return 1
				}
			case k.Pairwise() && k.Msg() == 3:
				if !lmac.VerifyMIC(kck, &k) {
					fmt.Println("  EAPOL: msg3 MIC INVALID — ignoring")
					continue
				}
				fmt.Println("  EAPOL: msg3 MIC ok")
				msg3 = &k
				goto haveMsg3
			}
		case <-deadline:
			fmt.Println("  EAPOL: no msg3 within 20s")
			return 1
		case <-ctx.Done():
			return 1
		}
	}
haveMsg3:

	// --- GTK ---
	gtk, gtkIdx, err := extractGTK(kek, msg3.KeyData)
	if err != nil {
		fmt.Printf("  EAPOL: GTK extract failed: %v (keydatalen=%d)\n", err, len(msg3.KeyData))
		return 1
	}
	fmt.Printf("  EAPOL: GTK unwrapped (%d bytes, idx %d)\n", len(gtk), gtkIdx)

	// --- msg4 ---
	msg4 := &lmac.KeyFrame{
		KeyInfo: lmac.KeyInfoVerHMACSHA1 | lmac.KeyInfoPairwise | lmac.KeyInfoMIC | lmac.KeyInfoSecure,
		Replay:  msg3.Replay,
	}
	if err := sendEapol(msg4); err != nil {
		log.Printf("send msg4: %v", err)
		return 1
	}
	fmt.Println("  EAPOL: msg4 sent, installing keys ...")

	// --- key install ---
	tk := ptk[32:48]
	submitKey := func(name string, msg lmac.Builder) bool {
		c, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		if err := s.submitter.Submit(c, msg); err != nil {
			fmt.Printf("  %s: NO CFM (%v)\n", name, err)
			return false
		}
		fmt.Printf("  %s: CFM ok\n", name)
		return true
	}
	submitKey("mm_key_add PTK", &lmac.KeyAddReq{
		KeyIdx: 0, StaIdx: apIdx, Key: append([]byte(nil), tk...),
		Cipher: lmac.CipherCCMP, VifIdx: vif, Pairwise: true,
	})
	submitKey("mm_key_add GTK", &lmac.KeyAddReq{
		KeyIdx: gtkIdx, StaIdx: 0xFF, Key: gtk,
		Cipher: lmac.CipherCCMP, VifIdx: vif, Pairwise: false,
	})
	submitKey("me_set_control_port open", lmac.SetControlPortReq{StaIdx: apIdx, Open: true})

	fmt.Println("  EAPOL: handshake complete — controlled port open")
	return 0
}

// extractGTK walks the msg3 key-data KDEs to the GTK KDE
// (0xdd, OUI 00-0f-ac, data-type 1) and unwraps it with KEK.
func extractGTK(kek, keyData []byte) ([]byte, uint8, error) {
	for off := 0; off+2 <= len(keyData); {
		eid, elen := keyData[off], int(keyData[off+1])
		if off+2+elen > len(keyData) {
			break
		}
		body := keyData[off+2 : off+2+elen]
		if eid == 0xdd && len(body) >= 8 &&
			body[0] == 0x00 && body[1] == 0x0f && body[2] == 0xac && body[3] == 0x01 {
			keyIdx := body[4] & 0x03
			gtk, err := lmac.UnwrapKey(kek, body[6:])
			if err != nil {
				return nil, 0, err
			}
			return gtk, keyIdx, nil
		}
		off += 2 + elen
	}
	// Fallback: the whole blob may be just the wrapped GTK.
	gtk, err := lmac.UnwrapKey(kek, keyData)
	if err != nil {
		return nil, 0, fmt.Errorf("no GTK KDE found and raw unwrap failed: %w", err)
	}
	return gtk, 0, nil
}
