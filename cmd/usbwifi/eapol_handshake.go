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
// runTxProbe tests raw TX delivery without any crypto: after association,
// wait for the first msg1, then send EAPOL-Logoff (type 2, no MIC) and watch
// whether the AP stops its msg1 retries. Retries stopping early proves our
// TX frames reach the AP (problem = msg2 content); identical retries prove
// TX is broken.
func runTxProbe(ctx context.Context, s *session, vif, apIdx uint8, bssid, staMAC [6]byte, eapolCh <-chan []byte, txOnMsgPipe bool) int {
	fmt.Println("  TXPROBE: waiting for first msg1 ...")
	deadline := time.After(30 * time.Second)
	for {
		select {
		case f := <-eapolCh:
			var k lmac.KeyFrame
			if err := k.Decode(f); err != nil {
				continue
			}
			if !k.Pairwise() || k.Msg() != 1 {
				continue
			}
			fmt.Printf("  TXPROBE: got msg1 (replay %d), sending EAPOL-Logoff ...\n", k.Replay)
			// 802.1X Logoff: ver=1, type=2, len=0.
			logoff := []byte{1, 2, 0, 0}
			tx := &lmac.TxData{DA: bssid, SA: staMAC,
				Ethertype: lmac.EAPOLEthertype, VifIdx: vif, StaIdx: apIdx,
				Payload: logoff}
			frame, err := tx.Encode()
			if err != nil {
				return 1
			}
			if txOnMsgPipe {
				err = s.sess.BulkOutDataMsg(ctx, frame)
			} else {
				err = s.sess.BulkOutData(ctx, frame)
			}
			if err != nil {
				fmt.Printf("  TXPROBE: logoff send failed: %v\n", err)
				return 1
			}
			fmt.Println("  TXPROBE: logoff sent; watching 25s for further msg1 ...")
			end := time.After(25 * time.Second)
			n := 0
			for {
				select {
				case f2 := <-eapolCh:
					var k2 lmac.KeyFrame
					if err := k2.Decode(f2); err == nil && k2.Pairwise() && k2.Msg() == 1 {
						n++
						fmt.Printf("  TXPROBE: post-logoff msg1 #%d (replay %d)\n", n, k2.Replay)
					}
				case <-end:
					fmt.Printf("  TXPROBE: done, %d msg1 after logoff\n", n)
					return 0
				case <-ctx.Done():
					return 1
				}
			}
		case <-deadline:
			fmt.Println("  TXPROBE: no msg1 within 30s")
			return 1
		case <-ctx.Done():
			return 1
		}
	}
}

func runEapolHandshake(ctx context.Context, s *session, vif, apIdx uint8, bssid, staMAC [6]byte, ssid, passphrase string, eapolCh <-chan []byte, txOnMsgPipe bool) int {
	fmt.Printf("  EAPOL: vif=%d apidx=%d sta=%02x:%02x:%02x:%02x:%02x:%02x\n",
		vif, apIdx, staMAC[0], staMAC[1], staMAC[2], staMAC[3], staMAC[4], staMAC[5])
	// Nonstandard but cheap: open the controlled port BEFORE the handshake
	// in case this firmware gates host TX data (even EAPOL) on it.
	{
		c, cancel := context.WithTimeout(ctx, 4*time.Second)
		err := s.submitter.Submit(c, lmac.SetControlPortReq{StaIdx: apIdx, Open: true})
		cancel()
		fmt.Printf("  EAPOL: pre-open control port err=%v\n", err)
	}
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
	fmt.Printf("  EAPOL: ANonce %x\n  EAPOL: SNonce %x\n", msg1.Nonce, sNonce)

	msg2 := &lmac.KeyFrame{
		KeyInfo: lmac.KeyInfoVerHMACSHA1 | lmac.KeyInfoPairwise | lmac.KeyInfoMIC,
		KeyLen:  msg1.KeyLen, // echo the cipher key length (16 for CCMP)
		Replay:  msg1.Replay,
		KeyData: append([]byte(nil), lmac.WPA2PSKCCMPRsnIE...),
	}
	copy(msg2.Nonce[:], sNonce[:])
	sendEapol := func(k *lmac.KeyFrame) error {
		k.MIC = [16]byte{} // a resend must not MIC over the previous MIC
		enc := k.Encode()
		for i := lmac.MICOffset; i < lmac.MICOffset+16 && i < len(enc); i++ {
			enc[i] = 0
		}
		mic := lmac.ComputeMIC(kck, enc)
		copy(k.MIC[:], mic)
		if k.KeyInfo&lmac.KeyInfoPairwise != 0 && k.Msg() == 2 {
			fmt.Printf("  EAPOL: msg2 frame %x\n", k.Encode())
		}
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
		// SETTLED: data frames go out the bulk data pipe. The old code
		// ALTERNATED pipes per send to work out which one transmitted; once the
		// record framing was fixed that hack became the bug. msg2 went out
		// bulk-out and was confirmed (TXCFM) and answered (msg3), then msg4 was
		// toggled onto the msg pipe, vanished, and the AP timed the handshake
		// out with SM_DISCONNECT_IND reason 15 — after we had already reported
		// "controlled port open". Keep --tx-msg-pipe as an explicit override,
		// but never alternate.
		pipe := "bulk-out"
		if txOnMsgPipe {
			pipe = "msg-out"
			err = s.sess.BulkOutDataMsg(ctx, frame)
		} else {
			err = s.sess.BulkOutData(ctx, frame)
		}
		fmt.Printf("  EAPOL: msg%d sent via %s (%d bytes, err=%v)\n", k.Msg(), pipe, len(frame), err)
		fmt.Printf("  EAPOL: tx record %x\n", frame)
		return err
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
	// msg3's whole Key Data field is one AES-key-wrapped blob: unwrap once,
	// then parse the plaintext KDE list. Walking KDEs over the ciphertext
	// finds nothing and silently yields a wrong key.
	gtk, gtkIdx, err := lmac.ParseKeyData(kek, msg3.KeyData,
		msg3.KeyInfo&lmac.KeyInfoEncrypted != 0)
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
	okPTK := submitKey("mm_key_add PTK", &lmac.KeyAddReq{
		KeyIdx: 0, StaIdx: apIdx, Key: append([]byte(nil), tk...),
		Cipher: lmac.CipherCCMP, VifIdx: vif, Pairwise: true,
	})
	okGTK := submitKey("mm_key_add GTK", &lmac.KeyAddReq{
		KeyIdx: gtkIdx, StaIdx: 0xFF, Key: gtk,
		Cipher: lmac.CipherCCMP, VifIdx: vif, Pairwise: false,
	})
	okPort := submitKey("me_set_control_port open", lmac.SetControlPortReq{StaIdx: apIdx, Open: true})

	// Report what actually happened. Claiming an open port on the strength of
	// three unchecked return values is the same failure this project already
	// fixed once for associations that never happened.
	if !okPTK || !okGTK || !okPort {
		fmt.Printf("  EAPOL: key install FAILED (ptk=%v gtk=%v port=%v) — controlled port NOT open\n",
			okPTK, okGTK, okPort)
		return 1
	}
	fmt.Println("  EAPOL: handshake complete — controlled port open")

	// The 4-way handshake establishes the PAIRWISE key. The group key has its
	// own lifetime, and the AP rekeys it on a timer with a separate 2-way
	// exchange. Without a handler for that, the link dies at the first rekey:
	// observed as SM_DISCONNECT_IND reason 16 ("group-key handshake timeout")
	// after about an hour, with the bridge still transmitting into a dead link
	// and its rx counter frozen.
	go maintainGroupKey(ctx, s, vif, apIdx, bssid, staMAC, kck, kek, eapolCh)
	return 0
}

// maintainGroupKey answers the AP's group-key (GTK) rekeys for the life of the
// link.
//
// The exchange is the pairwise handshake in miniature: the AP sends an
// EAPOL-Key with the Group bit clear of Pairwise, carrying an encrypted GTK;
// we verify the MIC with the KCK we already have, unwrap the key data with the
// KEK, install the new GTK, and echo a MIC'd reply on the same replay counter.
// Miss it and the AP deauthenticates.
func maintainGroupKey(
	ctx context.Context, s *session, vif, apIdx uint8,
	bssid, staMAC [6]byte, kck, kek []byte, eapolCh <-chan []byte,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-eapolCh:
			var k lmac.KeyFrame
			if err := k.Decode(f); err != nil {
				continue
			}
			// Pairwise traffic here would be a full reauthentication, which
			// this loop is not equipped to run; leave it alone.
			if k.Pairwise() || k.KeyInfo&lmac.KeyInfoMIC == 0 {
				continue
			}
			if !lmac.VerifyMIC(kck, &k) {
				fmt.Println("  EAPOL: group rekey MIC INVALID — ignoring")
				continue
			}
			gtk, gtkIdx, err := lmac.ParseKeyData(kek, k.KeyData,
				k.KeyInfo&lmac.KeyInfoEncrypted != 0)
			if err != nil {
				fmt.Printf("  EAPOL: group rekey — could not read the new GTK: %v\n", err)
				continue
			}

			// Install before acknowledging: the AP may start using the new key
			// as soon as our reply lands.
			c, cancel := context.WithTimeout(ctx, 4*time.Second)
			err = s.submitter.Submit(c, &lmac.KeyAddReq{
				KeyIdx: gtkIdx, StaIdx: 0xFF, Key: gtk,
				Cipher: lmac.CipherCCMP, VifIdx: vif, Pairwise: false,
			})
			cancel()
			if err != nil {
				fmt.Printf("  EAPOL: group rekey — MM_KEY_ADD got no CFM: %v\n", err)
				continue
			}

			// Reply: Group + MIC + Secure, same replay counter, no key data.
			reply := &lmac.KeyFrame{
				Version: k.Version,
				KeyInfo: lmac.KeyInfoVerHMACSHA1 | lmac.KeyInfoMIC | lmac.KeyInfoSecure |
					(k.KeyInfo & lmac.KeyInfoIndexMask),
				Replay: k.Replay,
			}
			reply.MIC = [16]byte{}
			enc := reply.Encode()
			for i := lmac.MICOffset; i < lmac.MICOffset+16 && i < len(enc); i++ {
				enc[i] = 0
			}
			copy(reply.MIC[:], lmac.ComputeMIC(kck, enc))

			tx := &lmac.TxData{
				DA: bssid, SA: staMAC, Ethertype: lmac.EAPOLEthertype,
				VifIdx: vif, StaIdx: apIdx, Payload: reply.Encode(),
			}
			frame, encErr := tx.Encode()
			if encErr != nil {
				continue
			}
			if err := s.sess.BulkOutData(ctx, frame); err != nil {
				fmt.Printf("  EAPOL: group rekey — reply send failed: %v\n", err)
				continue
			}
			fmt.Printf("  EAPOL: group rekey handled (new GTK idx %d, %d bytes)\n", gtkIdx, len(gtk))
		}
	}
}
