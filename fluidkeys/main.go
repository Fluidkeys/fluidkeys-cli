package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"

	"github.com/docopt/docopt-go"
	"github.com/fluidkeys/fluidkeys/backupzip"
	"github.com/fluidkeys/fluidkeys/colour"
	"github.com/fluidkeys/fluidkeys/config"
	"github.com/fluidkeys/fluidkeys/database"
	"github.com/fluidkeys/fluidkeys/fingerprint"
	"github.com/fluidkeys/fluidkeys/gpgwrapper"
	"github.com/fluidkeys/fluidkeys/humanize"
	"github.com/fluidkeys/fluidkeys/keyring"
	"github.com/fluidkeys/fluidkeys/keytableprinter"
	"github.com/fluidkeys/fluidkeys/out"
	"github.com/fluidkeys/fluidkeys/pgpkey"
	"github.com/fluidkeys/fluidkeys/scheduler"

	"github.com/sethvargo/go-diceware/diceware"
)

const DicewareNumberOfWords int = 6
const DicewareSeparator string = "."

const GPGMissing string = "GPG isn't working on your system 🤒\n"
const ContinueWithoutGPG string = "You can still use FluidKeys to make a key and then later import it from your backup.\n\nAlternatively, quit now [ctrl-c], install GPG then run FluidKeys again.\n"
const PromptPressEnter string = "Press enter to continue"

const PromptEmail string = "To start using Fluidkeys, first you'll need to create a key.\n\nEnter your email address, this will help other people find your key.\n"
const PromptFirstPassword string = "This is your password.\n\n* If you use a password manager, save it there now\n* Otherwise write it on a piece of paper and keep it with you\n"
const PromptLastPassword string = "That didn't match 🤷🏽 This is your last chance!\n"
const FailedToConfirmPassword string = "That didn't match. Quitting...\n"

const PromptWhichKeyFromGPG string = "Which key would you like to import?"

const Version = "0.1.5"

var (
	gpg                gpgwrapper.GnuPG
	fluidkeysDirectory string
	db                 database.Database
	Config             config.Config
	Keyring            keyring.Keyring
)

type DicewarePassword struct {
	words     []string
	separator string
}

func (d DicewarePassword) AsString() string {
	return strings.Join(d.words, d.separator)
}

type generatePgpKeyResult struct {
	pgpKey *pgpkey.PgpKey
	err    error
}

type exitCode = int

func init() {
	var err error
	fluidkeysDirectory, err = getFluidkeysDirectory()
	if err != nil {
		fmt.Printf("Failed to get fluidkeys directory: %v\n", err)
		os.Exit(1)
	}

	configPointer, err := config.Load(fluidkeysDirectory)
	if err != nil {
		fmt.Printf("Failed to open config file: %v\n", err)
		os.Exit(2)
	} else {
		Config = *configPointer
	}

	keyringPointer, err := keyring.Load()
	if err != nil {
		fmt.Printf("Failed to load keyring: %v\n", err)
		os.Exit(3)
	} else {
		Keyring = *keyringPointer
	}

	db = database.New(fluidkeysDirectory)
	gpg = gpgwrapper.GnuPG{}
	out.SetOutputToTerminal()
}

func ensureCrontabStateMatchesConfig() {
	if Config.RunFromCron() {
		crontabWasAdded, err := scheduler.Enable()
		if err != nil {
			panic(err)
		}

		if crontabWasAdded {
			printInfo(fmt.Sprintf("Added Fluidkeys to crontab.  Edit %s to remove.", Config.GetFilename()))
		}
	} else {
		crontabWasRemoved, err := scheduler.Disable()
		if err != nil {
			panic(err)
		}

		if crontabWasRemoved {
			printInfo(fmt.Sprintf("Removed Fluidkeys from crontab.  Edit %s to add again.", Config.GetFilename()))
		}
	}
}

func main() {
	usage := fmt.Sprintf(`Fluidkeys %s

Configuration file: %s

Usage:
	fk key create
	fk key from-gpg
	fk key list
	fk key rotate [--dry-run]
	fk key rotate automatic [--cron-output]

Options:
	-h --help         Show this screen
	   --dry-run      Don't change anything: only output what would happen
	   --cron-output  Only print output on errors`, // TODO: Document `automatic`
		Version,
		Config.GetFilename(),
	)

	args, _ := docopt.ParseDoc(usage)

	ensureCrontabStateMatchesConfig()

	switch getSubcommand(args, []string{"key"}) {
	case "init":
		os.Exit(initSubcommand(args))
	case "key":
		os.Exit(keySubcommand(args))
	}
}

func getSubcommand(args docopt.Opts, subcommands []string) string {
	// subcommands := []string{"init", "key"}

	for _, subcommand := range subcommands {
		value, err := args.Bool(subcommand)
		if err != nil {
			panic(err)
		}
		if value {
			return subcommand
		}
	}
	panic(fmt.Errorf("expected to find one of these subcommands: %v", subcommands))
}

func initSubcommand(args docopt.Opts) exitCode {
	out.Print("`init` subcommand not currently implemented.\n")
	return 1
}

func keySubcommand(args docopt.Opts) exitCode {
	switch getSubcommand(args, []string{
		"create", "from-gpg", "list", "rotate",
	}) {
	case "create":
		os.Exit(keyCreate())
	case "from-gpg":
		os.Exit(keyFromGpg())
	case "list":
		os.Exit(keyList())
	case "rotate":
		dryRun, err := args.Bool("--dry-run")
		if err != nil {
			panic(err)
		}
		automatic, err := args.Bool("automatic")
		if err != nil {
			panic(err)
		}
		cronOutput, err := args.Bool("--cron-output")
		if err != nil {
			panic(err)
		}
		os.Exit(keyRotate(dryRun, automatic, cronOutput))
	}
	panic(fmt.Errorf("keySubcommand got unexpected arguments: %v", args))
}

func keyFromGpg() exitCode {
	availableKeys, err := keysAvailableToGetFromGpg()
	if err != nil {
		out.Print(fmt.Sprintf("Failed to list available keys: %v", err))
		return 1
	}

	if len(availableKeys) == 0 {
		out.Print(fmt.Sprintf("No secret keys found in GPG\n"))
		return 1
	}

	out.Print(formatListedKeysForImportingFromGpg(availableKeys))
	keyToImport := promptForKeyToImportFromGpg(availableKeys)

	if keyToImport == nil {
		out.Print("No key selected to link\n")
		return 0
	}

	db.RecordFingerprintImportedIntoGnuPG(keyToImport.Fingerprint)
	Config.SetStorePassword(keyToImport.Fingerprint, false)
	Config.SetRotateAutomatically(keyToImport.Fingerprint, false)
	out.Print("The key has been linked to Fluidkeys\n")
	return keyList()
}

// keysAvailableToGetFromGpg returns a filtered slice of SecretKeyListings, removing
// any keys that Fluidkeys is already managing.
func keysAvailableToGetFromGpg() ([]gpgwrapper.SecretKeyListing, error) {

	importedFingerprints, err := db.GetFingerprintsImportedIntoGnuPG()
	if err != nil {
		return nil, fmt.Errorf("Failed to get fingerprints from database: %v\n", err)
	}

	var availableKeys []gpgwrapper.SecretKeyListing

	allGpgKeys, err := gpg.ListSecretKeys()
	if err != nil {
		return nil, fmt.Errorf("Error getting secret keys from GPG: %v", err)
	}

	for _, key := range allGpgKeys {
		if !fingerprint.Contains(importedFingerprints, key.Fingerprint) {
			availableKeys = append(availableKeys, key)
		}
	}
	return availableKeys, nil
}

func loadPgpKeys() ([]pgpkey.PgpKey, error) {
	fingerprints, err := db.GetFingerprintsImportedIntoGnuPG()
	if err != nil {
		return nil, err
	}

	var keys []pgpkey.PgpKey

	for _, fingerprint := range fingerprints {
		armoredPublicKey, err := gpg.ExportPublicKey(fingerprint)
		if err != nil {
			continue // skip this key. TODO: log?
		}

		pgpKey, err := pgpkey.LoadFromArmoredPublicKey(armoredPublicKey)
		if err != nil {
			continue // skip this key. TODO: log?
		}
		keys = append(keys, *pgpKey)
	}
	sort.Sort(pgpkey.ByCreated(keys))
	return keys, nil
}

func keyCreate() exitCode {

	if !gpg.IsWorking() {
		out.Print(colour.Warning("\n" + GPGMissing + "\n"))
		out.Print(ContinueWithoutGPG + "\n")
		promptForInput("Press enter to continue. ")
	}
	email := promptForEmail()
	channel := make(chan generatePgpKeyResult)
	go generatePgpKey(email, channel)

	password := generatePassword(DicewareNumberOfWords, DicewareSeparator)

	displayPassword(PromptFirstPassword, password)
	if !userConfirmedRandomWord(password) {
		displayPassword(PromptLastPassword, password)
		if !userConfirmedRandomWord(password) {
			out.Print(FailedToConfirmPassword)
			os.Exit(1)
		}
	}

	out.Print("Generating key for " + email + "\n\n")

	generateJob := <-channel

	if generateJob.err != nil {
		panic(fmt.Sprint("Failed to generate key: ", generateJob.err))
	}

	filename, err := backupzip.OutputZipBackupFile(fluidkeysDirectory, generateJob.pgpKey, password.AsString())
	if err != nil {
		out.Print(fmt.Sprintf("Failed to create backup ZIP file: %s", err))
	}
	directory, _ := filepath.Split(filename)
	out.Print(fmt.Sprintf("Full key backup saved in %s\n", directory))

	pushPrivateKeyBackToGpg(generateJob.pgpKey, password.AsString(), &gpg)
	out.Print("The new key has been imported into GnuPG, inspect it with:\n")
	out.Print(fmt.Sprintf(" > gpg --list-keys '%s'\n", email))

	fingerprint := generateJob.pgpKey.Fingerprint()
	out.Print("Fluidkeys has configured a " + colour.CommandLineCode("cron") + " task to automatically rotate this key for you from now on ♻️\n")
	out.Print("To do this has required storing the key's password in your operating system's keyring.\n")
	db.RecordFingerprintImportedIntoGnuPG(fingerprint)
	if err := tryEnableRotateAutomatically(generateJob.pgpKey, password.AsString()); err == nil {
		out.Print(colour.Success(" ▸   Successfully configured key to automatically rotate\n\n"))
	} else {
		out.Print(colour.Warning(" ▸   Failed to configure key to automatically rotate\n\n"))
	}
	return 0
}

func keyList() exitCode {
	keys, err := loadPgpKeys()
	if err != nil {
		panic(err)
	}

	out.Print("\n")
	keytableprinter.Print(keys)
	return 0
}

func displayName(key *pgpkey.PgpKey) string {
	displayName, err := key.Email()
	if err != nil {
		displayName = fmt.Sprintf("%s", key.Fingerprint())
	}
	return colour.Info(displayName)
}

func getFluidkeysDirectory() (string, error) {
	dirFromEnv := os.Getenv("FLUIDKEYS_DIR")

	if dirFromEnv != "" {
		return dirFromEnv, nil
	} else {
		return makeFluidkeysHomeDirectory()
	}
}

func makeFluidkeysHomeDirectory() (string, error) {
	homeDirectory, err := homedir.Dir()

	if err != nil {
		return "", err
	}

	fluidkeysDir := filepath.Join(homeDirectory, ".config", "fluidkeys")
	os.MkdirAll(fluidkeysDir, 0700)
	return fluidkeysDir, nil
}

func generatePgpKey(email string, channel chan generatePgpKeyResult) {
	key, err := pgpkey.Generate(email, time.Now(), nil)

	channel <- generatePgpKeyResult{key, err}
}

func formatListedKeysForImportingFromGpg(secretKeyListings []gpgwrapper.SecretKeyListing) string {
	str := fmt.Sprintf("Found %s in GnuPG:\n\n", humanize.Pluralize(len(secretKeyListings), "key", "keys"))
	for index, key := range secretKeyListings {
		str += printSecretKeyListing(index+1, key)
	}
	return str
}

func printSecretKeyListing(listNumber int, key gpgwrapper.SecretKeyListing) string {
	formattedListNumber := colour.Info(fmt.Sprintf("%-4s", (strconv.Itoa(listNumber) + ".")))
	output := fmt.Sprintf("%s%s\n", formattedListNumber, key.Fingerprint)
	output += fmt.Sprintf("    Created on %s\n", key.Created.Format("2 January 2006"))
	for _, uid := range key.Uids {
		output += fmt.Sprintf("      %v\n", uid)
	}
	output += fmt.Sprintf("\n")
	return output
}

func promptForKeyToImportFromGpg(secretKeyListings []gpgwrapper.SecretKeyListing) *gpgwrapper.SecretKeyListing {
	var selectedKey int
	if len(secretKeyListings) == 1 {
		onlyKey := secretKeyListings[0]
		prompter := interactiveYesNoPrompter{}

		if prompter.promptYesNo("Import key?", "y", nil) {
			return &onlyKey
		} else {
			return nil
		}
	} else {
		invalidEntry := fmt.Sprintf("Please select between 1 and %v.\n", len(secretKeyListings))
		for validInput := false; !validInput; {
			rangePrompt := colour.Info(fmt.Sprintf("[1-%v]", len(secretKeyListings)))
			input := promptForInput(fmt.Sprintf(PromptWhichKeyFromGPG + " " + rangePrompt + " "))
			if integerSelected, err := strconv.Atoi(input); err != nil {
				out.Print(invalidEntry)
			} else {
				if (integerSelected >= 1) && (integerSelected <= len(secretKeyListings)) {
					selectedKey = integerSelected - 1
					validInput = true
				} else {
					out.Print(invalidEntry)
				}
			}
		}
		return &secretKeyListings[selectedKey]
	}
}

func promptForInputWithPipes(prompt string, reader *bufio.Reader) string {
	out.Print(prompt)
	response, err := reader.ReadString('\n')
	if err != nil {
		panic(err)
	}
	out.Print("\n")
	return strings.TrimRight(response, "\n")
}

func promptForInput(prompt string) string {
	return promptForInputWithPipes(prompt, bufio.NewReader(os.Stdin))
}

func promptForEmail() string {
	out.Print(PromptEmail + "\n")
	return promptForInput("[email] : ")
}

func generatePassword(numberOfWords int, separator string) DicewarePassword {
	return DicewarePassword{
		words:     diceware.MustGenerate(numberOfWords),
		separator: separator,
	}
}

func displayPassword(message string, password DicewarePassword) {
	out.Print(message + "\n")
	out.Print("  " + colour.Info(password.AsString()) + "\n\n")

	promptForInput("Press enter when you've written it down. ")
}

func userConfirmedRandomWord(password DicewarePassword) bool {
	clearScreen()
	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(password.words))
	correctWord := password.words[randomIndex]
	wordOrdinal := humanize.Ordinal(randomIndex + 1)

	out.Print(fmt.Sprintf("Enter the %s word from your password\n\n", wordOrdinal))
	givenWord := promptForInput("[" + wordOrdinal + " word] : ")
	return givenWord == correctWord
}

func clearScreen() {
	out.Print("\033[H\033[2J")
}

// Max returns the larger of x or y.
func Max(x, y int) int {
	if x < y {
		return y
	}
	return x
}
