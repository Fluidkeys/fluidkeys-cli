// Copyright 2018 Paul Furley and Ian Drysdale
//
// This file is part of Fluidkeys Client which makes it simple to use OpenPGP.
//
// Fluidkeys Client is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Fluidkeys Client is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Fluidkeys Client.  If not, see <https://www.gnu.org/licenses/>.

package fk

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/fluidkeys/api/v1structs"
	"github.com/fluidkeys/fluidkeys/colour"
	"github.com/fluidkeys/fluidkeys/fingerprint"
	fp "github.com/fluidkeys/fluidkeys/fingerprint"
	"github.com/fluidkeys/fluidkeys/humanize"
	"github.com/fluidkeys/fluidkeys/out"
	"github.com/fluidkeys/fluidkeys/pgpkey"
	"github.com/gofrs/uuid"
)

func secretReceive() exitCode {
	out.Print("\n")
	keys, err := loadPgpKeys()
	var downloadedSecrets []secret

	if err != nil {
		printFailed("Couldn't load PGP keys")
		return 1
	}

	out.Print(colour.Info("Downloading secrets...") + "\n\n")

	sawError := false

	secretLister := client

	for _, key := range keys {
		if !Config.ShouldPublishToAPI(key.Fingerprint()) {
			message := "Key not uploaded to Fluidkeys, can't receive secrets"
			out.Print("⛔ " + displayName(&key) + ": " + colour.Warning(message) + "\n")
			continue
		}
		encryptedSecrets, err := downloadEncryptedSecrets(key.Fingerprint(), secretLister)
		if err != nil {
			switch err.(type) {
			case errNoSecretsFound:
				out.Print("📭 " + displayName(&key) + ": No secrets found\n")
			default:
				out.Print("📪 " + displayName(&key) + ": " + colour.Failure(err.Error()) + "\n")
			}
			continue
		}

		privateKey, _, err := getDecryptedPrivateKeyAndPassword(&key, &interactivePasswordPrompter{})
		if err != nil {
			message := fmt.Sprintf("Error getting private key and password: %s", err)
			out.Print("📪 " + displayName(&key) + ": " + colour.Failure(message) + "\n")
			continue
		}
		decryptedSecrets, secretErrors := decryptSecrets(encryptedSecrets, privateKey)

		out.Print("📬 " + displayName(&key) + ":\n")

		for i, secret := range decryptedSecrets {
			out.Print(formatSecretListItem(i+1, secret.decryptedContent))
		}
		downloadedSecrets = append(downloadedSecrets, decryptedSecrets...)

		out.Print(strings.Repeat(secretDividerRune, secretDividerLength) + "\n")

		if len(secretErrors) > 0 {
			output := humanize.Pluralize(len(secretErrors), "secret", "secrets") + " failed to download for " + displayName(&key) + ":\n"
			out.Print(colour.Failure(colour.StripAllColourCodes(output)))
			for _, error := range secretErrors {
				printFailed(error.Error())
			}
			sawError = true
		}
	}

	sawErrorDeletingSecret := false

	if len(downloadedSecrets) > 0 {
		prompter := interactiveYesNoPrompter{}
		out.Print("\n")
		if prompter.promptYesNo("Delete now?", "Y", nil) == true {
			for _, secret := range downloadedSecrets {
				err := client.DeleteSecret(secret.sentToFingerprint, secret.UUID.String())
				if err != nil {
					log.Printf("failed to delete secret '%s': %v", secret.UUID, err)
					sawErrorDeletingSecret = true
				}
			}
			if sawErrorDeletingSecret {
				printFailed("One or more errors deleting secrets")
			} else {
				printSuccess("Deleted all secrets")
			}
		}
	}

	if sawError {
		return 1
	}
	return 0
}

func downloadEncryptedSecrets(fingerprint fp.Fingerprint, secretLister listSecretsInterface) (
	secrets []v1structs.Secret, err error) {
	encryptedSecrets, err := secretLister.ListSecrets(fingerprint)
	if err != nil {
		return nil, err
	}
	if len(encryptedSecrets) == 0 {
		return nil, errNoSecretsFound{}
	}
	return encryptedSecrets, nil
}

func decryptSecrets(encryptedSecrets []v1structs.Secret, privateKey *pgpkey.PgpKey) (
	secrets []secret, secretErrors []error) {
	for _, encryptedSecret := range encryptedSecrets {
		secret := secret{
			sentToFingerprint: privateKey.Fingerprint(),
		}
		err := decryptAPISecret(encryptedSecret, &secret, privateKey)
		if err != nil {
			secretErrors = append(secretErrors, err)
		} else {
			secrets = append(secrets, secret)
		}
	}
	return secrets, secretErrors
}

func formatSecretListItem(listNumber int, decryptedContent string) (output string) {
	displayCounter := fmt.Sprintf(out.NoLogCharacter+" %d. ", listNumber)
	trimmedDivider := strings.Repeat(secretDividerRune, secretDividerLength-(1+len([]rune(displayCounter))))
	output = displayCounter + trimmedDivider + "\n"
	output = output + decryptedContent
	if !strings.HasSuffix(decryptedContent, "\n") {
		output = output + "\n"
	}
	return output
}

func decryptAPISecret(
	encryptedSecret v1structs.Secret, decryptedSecret *secret, privateKey decryptorInterface) error {

	if encryptedSecret.EncryptedContent == "" {
		return fmt.Errorf("encryptedSecret.EncryptedContent can not be empty")
	}
	if encryptedSecret.EncryptedMetadata == "" {
		return fmt.Errorf("encryptedSecret.EncryptedMetadata can not be empty")
	}
	if decryptedSecret == nil {
		return fmt.Errorf("decryptedSecret can not be nil")
	}
	if privateKey == nil {
		return fmt.Errorf("privateKey can not be nil")
	}

	decryptedContent, err := privateKey.DecryptArmoredToString(encryptedSecret.EncryptedContent)
	if err != nil {
		log.Printf("Failed to decrypt secret: %s", err)
		return err
	}

	metadata := v1structs.SecretMetadata{}
	jsonMetadata, err := privateKey.DecryptArmored(encryptedSecret.EncryptedMetadata)
	if err != nil {
		log.Printf("Failed to decrypt secret metadata: %s", err)
		return err
	}
	err = json.NewDecoder(jsonMetadata).Decode(&metadata)
	if err != nil {
		log.Printf("Failed to decode secret metadata: %s", err)
		return err
	}
	uuid, err := uuid.FromString(metadata.SecretUUID)
	if err != nil {
		log.Printf("Failed to parse uuid from string: %s", err)
		return err
	}

	decryptedSecret.decryptedContent = decryptedContent
	decryptedSecret.UUID = uuid

	return nil
}

func countDigits(i int) (count int) {
	iString := strconv.Itoa(i)
	return len(iString)
}

const (
	secretDividerRune   = "─"
	secretDividerLength = 30
)

type secret struct {
	decryptedContent  string
	UUID              uuid.UUID
	sentToFingerprint fingerprint.Fingerprint
}

type errListSecrets struct {
	originalError error
}

func (e errListSecrets) Error() string { return e.originalError.Error() }

type errNoSecretsFound struct{}

func (e errNoSecretsFound) Error() string { return "" }

type errDecryptPrivateKey struct {
	originalError error
}

func (e errDecryptPrivateKey) Error() string { return e.originalError.Error() }

type listSecretsInterface interface {
	ListSecrets(fingerprint fingerprint.Fingerprint) ([]v1structs.Secret, error)
}

type decryptorInterface interface {
	DecryptArmored(encrypted string) (io.Reader, error)
	DecryptArmoredToString(encrypted string) (string, error)
}
