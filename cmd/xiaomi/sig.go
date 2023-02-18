package xiaomi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

const key_size = 1024
const group_size = key_size / 8
const encrypt_group_size = group_size - 11

const publicCer = `-----BEGIN CERTIFICATE-----
MIICQTCCAaoCCQDab4c81p7I/jANBgkqhkiG9w0BAQQFADBqMQswCQYDVQQGEwJD
TjEQMA4GA1UECBMHQmVpSmluZzEQMA4GA1UEBxMHQmVpSmluZzEPMA0GA1UEChMG
eGlhb21pMQ0wCwYDVQQLEwRtaXVpMRcwFQYDVQQDEw5kZXYueGlhb21pLmNvbTAe
Fw0xMzA1MTUwMzMyNDJaFw0yMzA1MTMwMzMyNDJaMGAxCzAJBgNVBAYTAkNOMQsw
CQYDVQQIEwJCSjELMAkGA1UEBxMCQkoxDjAMBgNVBAoTBWNvbGluMQ4wDAYDVQQL
EwVjb2xpbjEXMBUGA1UEAxMOZGV2LnhpYW9taS5jb20wgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBAMBf5LzEiMy0i8LeENXU9v0bTF4coM/kLfK6RvjWS69/6tUx
NxJvjDFNbLsmU4xpF3qFY9RI0qyRf79pmKfYUeWomQCM/hKo2lKIbWV7/RVheZhE
C2yGbUMRygIzJq3AChBT2MO1a7bA9LINcv+xLmoy5+l3MnVwbVUpWsC/GI59AgMB
AAEwDQYJKoZIhvcNAQEEBQADgYEAQfYL1/EdtTXJthFzQxfdKt6y3Ts3b3waTn6o
d9b+LCcU8EzKHmFOAIpkqIOTvrhB3o/KXEMeMI0PiNHuFnHv9+VGQKiaPFQtb9Ds
T8iowNDb4G8rdUcoVaczUDbBMG9r5J45UCDxaEzcjp6J0xIS3v11JBK1PtAKHY6R
nEJIZuc=
-----END CERTIFICATE-----
`

func loadPublicKeyFromCert() (*rsa.PublicKey, error) {
	// certData, err := os.ReadFile(cerFilePath)
	// if err != nil {
	// 	return nil, err
	// }
	block, _ := pem.Decode([]byte(publicCer))
	if block == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("invalid public key type")
	}
	return publicKey, nil
}

func encryptByPublicKey(plaintext []byte, publicKey *rsa.PublicKey) (string, error) {
	encryptedData := make([]byte, 0)
	for len(plaintext) > 0 {
		var blockSize int
		if len(plaintext) > encrypt_group_size {
			blockSize = encrypt_group_size
		} else {
			blockSize = len(plaintext)
		}
		encryptedBlock, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, plaintext[:blockSize])
		if err != nil {
			return "", err
		}
		encryptedData = append(encryptedData, encryptedBlock...)
		plaintext = plaintext[blockSize:]
	}

	return fmt.Sprintf("%x\n", encryptedData), nil
}
