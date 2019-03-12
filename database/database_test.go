package database

import (
	"io/ioutil"
	"testing"

	"github.com/fluidkeys/fluidkeys/assert"
	fpr "github.com/fluidkeys/fluidkeys/fingerprint"
)

func TestRecordFingerprintImportedIntoGnuPG(t *testing.T) {

	t.Run("record works to an empty database", func(t *testing.T) {
		fingerprint := exampleFingerprintA
		database := New(makeTempDirectory(t))
		err := database.RecordFingerprintImportedIntoGnuPG(fingerprint)
		assert.NoError(t, err)

		importedFingerprints, err := database.GetFingerprintsImportedIntoGnuPG()
		assertContainsFingerprint(t, importedFingerprints, fingerprint)
	})

	t.Run("record appends a new key to a database with key ids already stored", func(t *testing.T) {
		existingFingerprint := exampleFingerprintA
		newFingerprint := exampleFingerprintB
		database := New(makeTempDirectory(t))

		err := database.RecordFingerprintImportedIntoGnuPG(existingFingerprint)
		assert.NoError(t, err)
		err = database.RecordFingerprintImportedIntoGnuPG(newFingerprint)
		assert.NoError(t, err)

		importedFingerprints, err := database.GetFingerprintsImportedIntoGnuPG()
		assertContainsFingerprint(t, importedFingerprints, existingFingerprint)
		assertContainsFingerprint(t, importedFingerprints, newFingerprint)
	})

	t.Run("doesn't duplicate key ids if trying to record a key that already is stored", func(t *testing.T) {
		fingerprint := exampleFingerprintA
		database := New(makeTempDirectory(t))

		err := database.RecordFingerprintImportedIntoGnuPG(fingerprint)
		assert.NoError(t, err)
		err = database.RecordFingerprintImportedIntoGnuPG(fingerprint)
		assert.NoError(t, err)

		importedFingerprints, err := database.GetFingerprintsImportedIntoGnuPG()
		if len(importedFingerprints) != 1 {
			t.Errorf("Expected 1 entry in slice, '%v'", importedFingerprints)
		}
	})
}

func TestGetFingerprintsImportedIntoGnuPG(t *testing.T) {

	t.Run("can read back fingerprint written to database", func(t *testing.T) {
		database := New(makeTempDirectory(t))
		fingerprint := exampleFingerprintA
		err := database.RecordFingerprintImportedIntoGnuPG(fingerprint)
		assert.NoError(t, err)

		importedFingerprints, err := database.GetFingerprintsImportedIntoGnuPG()
		assert.NoError(t, err)
		assertContainsFingerprint(t, importedFingerprints, fingerprint)
	})

}

func TestDeduplicateKeyImportedIntoGnuPGMessages(t *testing.T) {

	slice := []KeyImportedIntoGnuPGMessage{
		exampleKeyImportedMessageA,
		exampleKeyImportedMessageA,
		exampleKeyImportedMessageB,
		exampleKeyImportedMessageC,
	}

	got := deduplicateKeyImportedIntoGnuPGMessages(slice)
	want := []KeyImportedIntoGnuPGMessage{
		exampleKeyImportedMessageA,
		exampleKeyImportedMessageB,
		exampleKeyImportedMessageC,
	}

	if len(got) != len(want) {
		t.Errorf("Expected '%v' but got '%v'", want, got)
	}
}

func makeTempDirectory(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "db")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return dir
}

func assertContainsFingerprint(t *testing.T, slice []fpr.Fingerprint, element fpr.Fingerprint) {
	t.Helper()
	if !containsFingerprint(slice, element) {
		t.Fatalf("Expected '%v' to contain '%v'", slice, element)
	}
}

func containsFingerprint(s []fpr.Fingerprint, e fpr.Fingerprint) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

var exampleFingerprintA = fpr.MustParse("AAAA AAAA AAAA AAAA AAAA  AAAA AAAA AAAA AAAA AAAA")
var exampleFingerprintB = fpr.MustParse("BBBB BBBB BBBB BBBB BBBB  BBBB BBBB BBBB BBBB BBBB")
var exampleFingerprintC = fpr.MustParse("CCCC CCCC CCCC CCCC CCCC  CCCC CCCC CCCC CCCC CCCC")

var exampleKeyImportedMessageA = KeyImportedIntoGnuPGMessage{Fingerprint: exampleFingerprintA}
var exampleKeyImportedMessageB = KeyImportedIntoGnuPGMessage{Fingerprint: exampleFingerprintB}
var exampleKeyImportedMessageC = KeyImportedIntoGnuPGMessage{Fingerprint: exampleFingerprintC}
