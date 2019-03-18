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

package database

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	fpr "github.com/fluidkeys/fluidkeys/fingerprint"
	"github.com/fluidkeys/fluidkeys/team"
	"github.com/gofrs/uuid"
)

// Database is the user's Fluidkeys database. It points at the filepath for the jsonFilename
type Database struct {
	jsonFilename string
}

// Message is the structure the database takes
type Message struct {
	KeysImportedIntoGnuPG []KeyImportedIntoGnuPGMessage
	RequestsToJoinTeams   []RequestToJoinTeamMessage
}

// KeyImportedIntoGnuPGMessage represents a key the user has imported into GnuPG from Fluidkeys
type KeyImportedIntoGnuPGMessage struct {
	Fingerprint fpr.Fingerprint
}

// RequestToJoinTeamMessage records a request to join a team.
type RequestToJoinTeamMessage struct {
	TeamUUID    uuid.UUID       `json: "TeamUUID"`
	Fingerprint fpr.Fingerprint `json: "Fingerprint"`
	RequestedAt time.Time       `json: "RequestedAt"`
}

// New returns a database from the given fluidkeys directory
func New(fluidkeysDirectory string) Database {
	jsonFilename := filepath.Join(fluidkeysDirectory, "db.json")
	return Database{jsonFilename: jsonFilename}
}

// RecordFingerprintImportedIntoGnuPG takes a given fingperprint and records that it's been
// imported into GnuPG by writing an updated json database.
func (db *Database) RecordFingerprintImportedIntoGnuPG(newFingerprint fpr.Fingerprint) error {
	message, err := db.loadFromFile()
	if err != nil {
		return err
	}

	existingKeysImported := message.KeysImportedIntoGnuPG

	message.KeysImportedIntoGnuPG = deduplicateKeyImportedIntoGnuPGMessages(
		append(existingKeysImported, KeyImportedIntoGnuPGMessage{
			Fingerprint: newFingerprint,
		}),
	)

	return db.saveToFile(*message)
}

// RecordRequestToJoinTeam takes a given request to join a team and records that it's been
// sent by writing an updated json database.
func (db *Database) RecordRequestToJoinTeam(
	teamUUID uuid.UUID, fingerprint fpr.Fingerprint, now time.Time) error {

	message, err := db.loadFromFile()
	if err != nil {
		return err
	}

	newRequest := RequestToJoinTeamMessage{
		TeamUUID:    teamUUID,
		Fingerprint: fingerprint,
		RequestedAt: now,
	}

	message.RequestsToJoinTeams = append(message.RequestsToJoinTeams, newRequest)

	return db.saveToFile(*message)
}

// GetFingerprintsImportedIntoGnuPG returns a slice of fingerprints that have
// been imported into GnuPG
func (db *Database) GetFingerprintsImportedIntoGnuPG() (fingerprints []fpr.Fingerprint, err error) {
	message, err := db.loadFromFile()
	if os.IsNotExist(err) {
		return []fpr.Fingerprint{}, nil
	}
	if err != nil {
		return nil, err
	}

	for _, v := range message.KeysImportedIntoGnuPG {
		fingerprints = append(fingerprints, v.Fingerprint)
	}

	return fingerprints, nil
}

// GetRequestsToJoinTeams returns a slice of requests to join teams the user has made.
func (db *Database) GetRequestsToJoinTeams() (requests []team.RequestToJoinTeam, err error) {
	message, err := db.loadFromFile()
	if os.IsNotExist(err) {
		return []team.RequestToJoinTeam{}, nil
	}
	if err != nil {
		return nil, err
	}

	for _, msg := range message.RequestsToJoinTeams {
		requests = append(requests, team.RequestToJoinTeam{
			TeamUUID:    msg.TeamUUID,
			Fingerprint: msg.Fingerprint,
			RequestedAt: msg.RequestedAt,
		})
	}
	return requests, nil
}

// DeleteRequestToJoinTeam deletes all requests to join the team matching the given team UUID and
// fingerprint.
func (db *Database) DeleteRequestToJoinTeam(teamUUID uuid.UUID, fingerprint fpr.Fingerprint) error {
	message, err := db.loadFromFile()
	if err != nil {
		return err
	}

	newRequests := []RequestToJoinTeamMessage{}

	for _, req := range message.RequestsToJoinTeams {
		if req.TeamUUID == teamUUID && req.Fingerprint == fingerprint {
			log.Printf("deleting request to join team: %v", req)
			continue
		}

		newRequests = append(newRequests, req)
	}
	message.RequestsToJoinTeams = newRequests
	return db.saveToFile(*message)
}

func (db *Database) loadFromFile() (message *Message, err error) {
	file, err := os.Open(db.jsonFilename)
	if err != nil {
		if os.IsNotExist(err) {
			return &Message{}, nil
		}
		return nil, fmt.Errorf("couldn't open '%s': %v", db.jsonFilename, err)
	}

	byteValue, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ioutil.ReadAll(..) error: %v", err)
	}

	if err := json.Unmarshal(byteValue, &message); err != nil {
		return nil, fmt.Errorf("error loading json: %v", err)
	}

	return &Message{
		KeysImportedIntoGnuPG: deduplicateKeyImportedIntoGnuPGMessages(
			message.KeysImportedIntoGnuPG,
		),
		RequestsToJoinTeams: message.RequestsToJoinTeams,
	}, nil
}

func (db Database) saveToFile(message Message) error {
	file, err := os.Create(db.jsonFilename)
	if err != nil {
		return fmt.Errorf("couldn't open '%s': %v", db.jsonFilename, err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "    ")

	return encoder.Encode(message)
}

func deduplicateKeyImportedIntoGnuPGMessages(slice []KeyImportedIntoGnuPGMessage,
) (deduped []KeyImportedIntoGnuPGMessage) {

	alreadySeen := make(map[KeyImportedIntoGnuPGMessage]bool)

	for _, v := range slice {
		if _, inMap := alreadySeen[v]; !inMap {
			deduped = append(deduped, v)
			alreadySeen[v] = true
		}
	}
	return deduped
}
