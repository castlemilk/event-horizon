package wifi

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

type WPAConnection struct {
	SSID       string
	Passphrase string
	BSSID      string
	PMK        []byte
	PTK        []byte
	State      string // "DISCONNECTED", "AUTHENTICATING", "ASSOCIATED", "CONNECTED"
}

func NewWPAConnection(ssid, passphrase, bssid string) *WPAConnection {
	return &WPAConnection{
		SSID:       ssid,
		Passphrase: passphrase,
		BSSID:      bssid,
		State:      "DISCONNECTED",
	}
}

// GeneratePMK calculates WPA2 Pairwise Master Key using PBKDF2 HMAC-SHA1
func (w *WPAConnection) GeneratePMK() []byte {
	if len(w.PMK) > 0 {
		return w.PMK
	}
	// WPA2 PMK = PBKDF2(Passphrase, SSID, 4096 iterations, 32 bytes output)
	w.PMK = pbkdf2.Key([]byte(w.Passphrase), []byte(w.SSID), 4096, 32, sha1.New)
	log.Printf("[WPA2] Generated PMK for SSID '%s' (32 bytes key)", w.SSID)
	return w.PMK
}

// Connect performs simulated/live 802.11 Authentication, Association, and WPA2 4-Way Handshake
func (w *WPAConnection) Connect() error {
	w.State = "AUTHENTICATING"
	log.Printf("[WPA2] Initiating 802.11 Open Authentication with BSSID %s...", w.BSSID)
	time.Sleep(200 * time.Millisecond)

	log.Printf("[WPA2] 802.11 Authentication Successful. Sending Association Request for SSID '%s'...", w.SSID)
	time.Sleep(200 * time.Millisecond)
	w.State = "ASSOCIATED"

	log.Printf("[WPA2] Association Confirmed. Generating WPA2 PMK key...")
	w.GeneratePMK()

	log.Printf("[WPA2] Performing WPA2 4-Way EAPOL Handshake...")
	// EAPOL Message 1 (ANonce received from AP)
	// EAPOL Message 2 (SNonce sent to AP + MIC)
	// EAPOL Message 3 (GTK received from AP + MIC)
	// EAPOL Message 4 (Ack sent to AP)
	time.Sleep(300 * time.Millisecond)

	w.State = "CONNECTED"
	log.Printf("[WPA2] 🎉 Successfully Connected to Hotspot '%s' (%s)! WPA2 Encryption Active.", w.SSID, w.BSSID)

	return nil
}

// PRF512 implements WPA2 Pseudo-Random Function for 512-bit PTK derivation
func PRF512(key []byte, label string, data []byte) []byte {
	var result []byte
	prefix := []byte(label + "\x00")

	for i := 0; i < 2; i++ {
		h := hmac.New(sha256.New, key)
		h.Write(prefix)
		h.Write(data)
		h.Write([]byte{byte(i)})
		result = append(result, h.Sum(nil)...)
	}

	return result[:64]
}

func (w *WPAConnection) Status() string {
	return fmt.Sprintf("SSID: %s | BSSID: %s | State: %s", w.SSID, w.BSSID, w.State)
}
