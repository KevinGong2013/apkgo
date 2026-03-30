package cmd

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/scrypt"
)

func init() {
	configCmd.AddCommand(configExportCmd)
	configCmd.AddCommand(configImportCmd)
	rootCmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage encrypted config for sync across machines",
}

var configExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Encrypt and export config file",
	Example: `  apkgo config export -o config.enc
  APKGO_CONFIG_KEY=mypass apkgo config export -o config.enc`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outPath, _ := cmd.Flags().GetString("out")
		if outPath == "" {
			return fmt.Errorf("--out / -o is required")
		}

		plain, err := os.ReadFile(flagConfig)
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}

		passphrase, err := getPassphrase("Enter passphrase: ")
		if err != nil {
			return err
		}

		encrypted, err := encrypt(plain, passphrase)
		if err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}

		if err := os.WriteFile(outPath, encrypted, 0644); err != nil {
			return fmt.Errorf("write: %w", err)
		}

		writeOutput(map[string]string{
			"exported": outPath,
			"source":   flagConfig,
		})
		return nil
	},
}

var configImportCmd = &cobra.Command{
	Use:   "import <encrypted-file>",
	Short: "Decrypt and import config file",
	Args:  cobra.ExactArgs(1),
	Example: `  apkgo config import config.enc
  APKGO_CONFIG_KEY=mypass apkgo config import config.enc`,
	RunE: func(cmd *cobra.Command, args []string) error {
		encPath := args[0]

		data, err := os.ReadFile(encPath)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		passphrase, err := getPassphrase("Enter passphrase: ")
		if err != nil {
			return err
		}

		plain, err := decrypt(data, passphrase)
		if err != nil {
			return fmt.Errorf("decrypt failed (wrong passphrase?): %w", err)
		}

		if err := os.WriteFile(flagConfig, plain, 0644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		writeOutput(map[string]string{
			"imported": flagConfig,
			"source":   encPath,
		})
		return nil
	},
}

func init() {
	configExportCmd.Flags().String("out", "", "output file path (required)")
}

// getPassphrase reads from APKGO_CONFIG_KEY env var or prompts on stderr.
func getPassphrase(prompt string) (string, error) {
	if key := os.Getenv("APKGO_CONFIG_KEY"); key != "" {
		return key, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	var pass string
	if _, err := fmt.Fscanln(os.Stdin, &pass); err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	pass = strings.TrimSpace(pass)
	if pass == "" {
		return "", fmt.Errorf("passphrase cannot be empty")
	}
	return pass, nil
}

// encrypt uses scrypt + AES-256-GCM.
// Format: salt(32) + nonce(12) + ciphertext
func encrypt(plain []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plain, nil)

	// salt + nonce + ciphertext
	result := make([]byte, 0, len(salt)+len(nonce)+len(ciphertext))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)
	return result, nil
}

func decrypt(data []byte, passphrase string) ([]byte, error) {
	if len(data) < 44 { // 32 salt + 12 nonce minimum
		return nil, fmt.Errorf("data too short")
	}

	salt := data[:32]
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < 32+nonceSize {
		return nil, fmt.Errorf("data too short")
	}

	nonce := data[32 : 32+nonceSize]
	ciphertext := data[32+nonceSize:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}

func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	// scrypt with recommended params: N=32768, r=8, p=1, keyLen=32
	h := sha256.Sum256([]byte(passphrase))
	return scrypt.Key(h[:], salt, 32768, 8, 1, 32)
}
