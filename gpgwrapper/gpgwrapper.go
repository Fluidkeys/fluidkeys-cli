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

// gpgwrapper calls out to the system GnuPG binary

package gpgwrapper

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	fpr "github.com/fluidkeys/fluidkeys/fingerprint"

	"github.com/mitchellh/go-homedir"
)

var errNoVersionStringFound = errors.New("version string not found in GPG output")
var errNoHomeDirectoryStringFound = errors.New("home directory string not found in GPG output")

func ErrProblemExecutingGPG(gpgStdout string, arguments ...string) error {
	return fmt.Errorf("error executing GPG with %s: %s", arguments, gpgStdout)
}

var versionRegexp = regexp.MustCompile(`gpg \(GnuPG.*\) (\d+\.\d+\.\d+)`)
var homeRegexp = regexp.MustCompile(`Home: +([^\r\n]+)`)

// GnuPG provides methods to access the user's installation of GnuPG
type GnuPG struct {
	// fullGpgPath is the full path (e.g. /usr/bin/gpg2) to the GnuPG binary.
	// It is set during Load.
	fullGpgPath string

	homeDir string
}

// KeyListing refers to a key parsed from running `gpg --list-[secret]-keys`
type KeyListing struct {

	// Fingerprint is the human-readable format of the fingerprint of the
	// primary key, for example:
	// `AB01 AB01 AB01 AB01 AB01  AB01 AB01 AB01 AB01 AB01`
	Fingerprint fpr.Fingerprint

	// Uids is a list of UTF-8 user ID strings as defined in
	// https://tools.ietf.org/html/rfc4880#section-5.11
	Uids []string

	// Created is the time the key was apparently created in UTC.
	Created time.Time
}

// Load find's the user's gpg binary and returns a GnuPG struct referencing it
func Load() (*GnuPG, error) {
	gpgBinary, err := findGpgBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to find gpg: %v", err)
	}
	return &GnuPG{fullGpgPath: gpgBinary}, nil
}

// Version returns the GnuPG version string, e.g. "1.2.3"
func (g *GnuPG) Version() (string, error) {
	outString, _, err := g.run("", "--version")

	if err != nil {
		return "", err
	}

	version, err := parseVersionString(outString)

	if err != nil {
		err = fmt.Errorf("problem parsing version string, %v", err)
		return "", err
	}

	return version, nil
}

// HomeDir returns the GnuPG home directory, e.g. "/Users/jane/.gnupg"
func (g *GnuPG) HomeDir() (string, error) {
	outString, _, err := g.run("", "--version")
	if err != nil {
		return "", err
	}

	match := homeRegexp.FindStringSubmatch(outString)
	if match == nil {
		return "", errNoHomeDirectoryStringFound
	}

	if fullPath, err := homedir.Expand(match[1]); err != nil {
		return "", fmt.Errorf("error expanding home directory '%s': %v", match[1], err)
	} else {
		return fullPath, nil
	}
}

// IsWorking checks whether GPG is working
func (g *GnuPG) IsWorking() bool {
	_, err := g.Version()

	if err != nil {
		return false
	}

	return true
}

// ImportArmoredKey imports the given armored key into the GPG key ring
func (g *GnuPG) ImportArmoredKey(armoredKey string) error {
	_, _, err := g.run(armoredKey, "--import")
	if err != nil {
		return err
	}
	// TODO: are we correctly checking if GPG failed? I think it can return
	// exit code 0 *but* set stderr to communicate a problem

	return nil
}

// ListSecretKeys lists the secret(private) keys in the users key ring.
func (g *GnuPG) ListSecretKeys() ([]KeyListing, error) {
	args := []string{
		"--with-colons",
		"--with-fingerprint",
		"--fixed-list-mode",
		"--list-secret-keys",
	}
	outString, _, err := g.run("", args...)
	if err != nil {
		return nil, err
	}

	return parseListSecretKeys(outString)
}

// ListPublicKeys lists the public keys in the users key ring that match a given search string.
func (g *GnuPG) ListPublicKeys(searchString string) ([]KeyListing, error) {
	if searchString == "" {
		return nil, fmt.Errorf("no search string provided")
	}
	args := []string{
		"--with-colons",
		"--with-fingerprint",
		"--fixed-list-mode",
		"--list-keys",
		searchString,
	}

	stdOut, stdErr, err := g.run("", args...)
	if err != nil {
		if strings.Contains(stdErr, noPublicKey) {
			return []KeyListing{}, nil
		}
		return nil, err
	}

	return parseListPublicKeys(stdOut)
}

// ExportPublicKey returns 1 ascii armored public key for the given
// fingerprint
func (g *GnuPG) ExportPublicKey(fingerprint fpr.Fingerprint) (string, error) {
	args := []string{
		"--export-options", "export-minimal",
		"--armor",
		"--export",
		fingerprint.Hex(),
	}

	stdout, _, err := g.run("", args...)
	if err != nil {
		return "", err
	}

	if strings.Contains(stdout, nothingExported) {
		return "", fmt.Errorf("GnuPG returned 'nothing exported' for fingerprint '%s'", fingerprint)
	}

	numHeaders := strings.Count(stdout, publicHeader)
	numFooters := strings.Count(stdout, publicFooter)

	if numHeaders != 1 || numFooters != 1 {
		return "", fmt.Errorf(
			"Expected exactly 1 ascii-armored public key, got %d headers and %d footers",
			numHeaders, numFooters)
	}

	return stdout, nil
}

// ExportPrivateKey returns 1 ascii armored private key for the given
// fingerprint, assuming it is encrypted with the given password.
// The outputted private key is encrypted with the password.
func (g *GnuPG) ExportPrivateKey(fingerprint fpr.Fingerprint, password string) (string, error) {

	stdout, stderr, err := g.run(
		password,
		getArgsExportPrivateKeyWithPinentry(fingerprint)...,
	)

	if err != nil {
		if strings.Contains(stderr, invalidOptionPinentryMode) {
			stdout, stderr, err := g.run(
				password,
				getArgsExportPrivateKeyWithoutPinentry(fingerprint)...,
			)

			if err != nil {
				return stderr, err
			}

			return checkValidExportPrivateOutput(stdout, stderr)
		} else if strings.Contains(stderr, loopbackUnsupported) {
			if version, err := g.Version(); err == nil && version == "2.1.11" {
				return "", fmt.Errorf("for gpg-2.1.11, please see https://fluidkeys.com/tweak-gpg-2.1.11/")
			}
		} else if strings.Contains(stderr, badPassphrase) || strings.Contains(stderr, noPassphrase) {
			return stderr, &BadPasswordError{}
		}

		return stderr, err
	}

	return checkValidExportPrivateOutput(stdout, stderr)
}

func getArgsExportPrivateKeyWithPinentry(fingerprint fpr.Fingerprint) []string {
	return []string{
		"--pinentry-mode", "loopback", // don't use OS password prompt
		"--passphrase-fd", "0", // read password from stdin
		"--armor",
		"--export-secret-keys",
		fingerprint.Hex(),
	}
}

func getArgsExportPrivateKeyWithoutPinentry(fingerprint fpr.Fingerprint) []string {
	return []string{
		"--passphrase-fd", "0", // read password from stdin
		"--armor",
		"--export-secret-keys",
		fingerprint.Hex(),
	}
}

// checkValidExportPrivateOutput takes the output of `gpg --export-secret-key ...`
// and ensures:
// 1. there's exactly 1 ascii-armored secret key
// 2. there's no GnuPG warning message in stderr
//
// then it returns the output with err=nil if everything looks good.
func checkValidExportPrivateOutput(stdout string, stderr string) (string, error) {

	if strings.Contains(stderr, nothingExported) {
		return "", fmt.Errorf("GnuPG returned 'nothing exported'")
	}

	numHeaders := strings.Count(stdout, privateHeader)
	numFooters := strings.Count(stdout, privateFooter)

	if numHeaders != 1 || numFooters != 1 {
		return "", fmt.Errorf(
			"Expected exactly 1 ascii-armored secret key, got %d headers and %d footers",
			numHeaders, numFooters)
	}

	return stdout, nil
}

func parseVersionString(gpgStdout string) (string, error) {
	match := versionRegexp.FindStringSubmatch(gpgStdout)

	if match == nil {
		return "", errNoVersionStringFound
	}

	return match[1], nil
}

// run runs the given command, sends textToSend via stdin, and returns
// stdout, stderr and any error encountered
func (g *GnuPG) run(textToSend string, arguments ...string) (
	stdout string, stderr string, returnErr error) {
	fullArguments := g.prependGlobalArguments(arguments...)
	cmd := exec.Command(g.fullGpgPath, fullArguments...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		returnErr = fmt.Errorf("Failed to get stdout pipe '%s'", err)
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		returnErr = fmt.Errorf("Failed to get stderr pipe '%s'", err)
		return
	}

	if textToSend != "" {
		stdin, err := cmd.StdinPipe() // used to send textToSend
		if err != nil {
			returnErr = fmt.Errorf("Failed to get stdin pipe '%s'", err)
			return
		}

		if _, err = io.WriteString(stdin, textToSend); err != nil {
			return "", "", fmt.Errorf("failed to write text to stdin: %v", err)
		}

		if err := stdin.Close(); err != nil {
			return "", "", fmt.Errorf("failed to close stdin: %v", err)
		}
	}

	if err = cmd.Start(); err != nil {
		returnErr = fmt.Errorf("error starting gpg: %v", err)
		return
	}

	if stdoutBytes, err := ioutil.ReadAll(stdoutPipe); err != nil {
		returnErr = fmt.Errorf("error reading stdout: %v", err)
		return
	} else {
		stdout = string(stdoutBytes)
	}

	if stderrBytes, err := ioutil.ReadAll(stderrPipe); err != nil {
		returnErr = fmt.Errorf("error reading stderr: %v", err)
		return
	} else {
		stderr = string(stderrBytes)
	}

	if err := cmd.Wait(); err != nil {
		// a non-zero exit code error from .Wait() looks like:
		// "exit status 2"

		stderrLines := strings.Split(
			strings.TrimRight(stderr, "\n\r"),
			"\n",
		)
		extraErr := ""

		switch len(stderrLines) {
		case 0:
			extraErr = ""

		case 1:
			extraErr = fmt.Sprintf(", stderr: %s", stderrLines[0])

		default:
			extraErr = fmt.Sprintf(", stderr: %s [see fluidkeys log for more]", stderrLines[0])
		}

		log.Printf("command failed: `gpg %s` : %s", strings.Join(fullArguments, " "), err)
		for _, line := range stderrLines {
			log.Print(line)
		}

		returnErr = fmt.Errorf("%v%s", err, extraErr)
		return
	}

	return
}

func (g *GnuPG) prependGlobalArguments(arguments ...string) []string {
	var globalArguments = []string{
		"-vv",
		"--keyid-format", "0xlong",
		"--batch",
		"--no-tty",
	}
	if g.homeDir != "" {
		homeDirArgs := []string{"--homedir", g.homeDir}
		globalArguments = append(globalArguments, homeDirArgs...)
	}
	return append(globalArguments, arguments...)
}

func findGpgBinary() (fullPath string, err error) {
	for _, fullPath := range gpgBinaryLocations {
		testGpg := GnuPG{fullGpgPath: fullPath}

		version, err := testGpg.Version()
		if err != nil {
			continue
		}

		if version[0:2] != "2." {
			log.Printf("ignoring %s (version %s, looking for gpg 2.x)", fullPath, version)
			continue
		}

		log.Printf("found working gpg2 with version '%s': %s", version, fullPath)
		return fullPath, nil
	}

	return "", fmt.Errorf("didn't find working `gpg2` or `gpg` binary with version 2.x")
}

var gpgBinaryLocations = []string{
	"/usr/bin/gpg2",
	"/usr/bin/gpg",
	"/usr/local/bin/gpg2",
	"/usr/local/bin/gpg",
	"/usr/local/MacGPG2/bin/gpg2",
	"/usr/local/MacGPG2/bin/gpg",
}

const (
	publicHeader              = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	publicFooter              = "-----END PGP PUBLIC KEY BLOCK-----"
	privateHeader             = "-----BEGIN PGP PRIVATE KEY BLOCK-----"
	privateFooter             = "-----END PGP PRIVATE KEY BLOCK-----"
	nothingExported           = "WARNING: nothing exported"
	invalidOptionPinentryMode = `gpg: invalid option "--pinentry-mode"`
	loopbackUnsupported       = `setting pinentry mode 'loopback' failed: Not supported`
	badPassphrase             = "Bad passphrase"
	noPassphrase              = "No passphrase given"
	noPublicKey               = "No public key"
)
