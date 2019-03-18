package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fluidkeys/api/v1structs"
	"github.com/fluidkeys/crypto/openpgp"
	"github.com/fluidkeys/crypto/openpgp/armor"
	"github.com/fluidkeys/fluidkeys/assert"
	"github.com/fluidkeys/fluidkeys/exampledata"
	fpr "github.com/fluidkeys/fluidkeys/fingerprint"
	"github.com/fluidkeys/fluidkeys/pgpkey"
	"github.com/fluidkeys/fluidkeys/team"
	"github.com/gofrs/uuid"
)

// String is a helper routine that allocates a new string value
// to store v and returns a pointer to it.
func String(v string) *string { return &v }

func TestGetPublicKey(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	t.Run("with valid JSON response", func(t *testing.T) {
		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprint(w, `{"armoredPublicKey": "---- BEGIN PGP PUBLIC KEY..."}`)
		}

		mux.HandleFunc("/email/jane@example.com/key", mockResponseHandler)

		armoredPublicKey, err := client.GetPublicKey("jane@example.com")

		assert.NoError(t, err)

		want := "---- BEGIN PGP PUBLIC KEY..."
		if armoredPublicKey != want {
			t.Errorf("GetPublicKey(\"jane@example.com\") returned %+v, want %+v", armoredPublicKey, want)
		}
	})

	t.Run("with empty response", func(t *testing.T) {
		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			// empty response
		}
		mux.HandleFunc("/email/joe@example.com/key", mockResponseHandler)

		armoredPublicKey, err := client.GetPublicKey("joe@example.com")

		assert.NoError(t, err)

		want := ""
		if armoredPublicKey != want {
			t.Errorf("GetPublicKey(\"joe@example.com\") returned %+v, want %+v", armoredPublicKey, want)
		}
	})

	t.Run("with a server error", func(t *testing.T) {
		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprint(w, `{"detail": "Key not found"}`)
		}
		mux.HandleFunc("/email/abby@example.com/key", mockResponseHandler)

		_, err := client.GetPublicKey("abby@example.com")

		assert.GotError(t, err)
	})
}

func TestGetPublicKeyByFingerprint(t *testing.T) {
	t.Run("responds with a good armored pgp key with matching fingerprint", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, exampledata.ExamplePublicKey4)
		}
		mux.HandleFunc(
			"/key/"+exampledata.ExampleFingerprint4.Hex()+".asc",
			mockResponseHandler,
		)

		key, err := client.GetPublicKeyByFingerprint(exampledata.ExampleFingerprint4)

		assert.NoError(t, err)
		assert.Equal(t, exampledata.ExampleFingerprint4, key.Fingerprint())
	})

	t.Run("responds with an armored pgp key with the wrong fingerprint", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, exampledata.ExamplePublicKey3)
		}
		mux.HandleFunc(
			"/key/"+exampledata.ExampleFingerprint4.Hex()+".asc",
			mockResponseHandler,
		)

		_, err := client.GetPublicKeyByFingerprint(exampledata.ExampleFingerprint4)

		assert.GotError(t, err)
		assert.Equal(t, fmt.Errorf("requested key BB3C 44BF 188D 56E6 35F4  A092 F73D 2F05 "+
			"33D7 F9D6 but got back 7C18 DE4D E478 1356 8B24  3AC8 719B D63E F03B DC20"), err)
	})

	t.Run("empty response body", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "")
		}
		mux.HandleFunc(
			"/key/"+exampledata.ExampleFingerprint4.Hex()+".asc",
			mockResponseHandler,
		)

		_, err := client.GetPublicKeyByFingerprint(exampledata.ExampleFingerprint4)

		assert.GotError(t, err)
		assert.Equal(t, fmt.Errorf("got http 200, but with empty body"), err)
	})

	t.Run("404 gives a specific error", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "")
		}
		mux.HandleFunc(
			"/key/"+exampledata.ExampleFingerprint4.Hex()+".asc",
			mockResponseHandler,
		)

		_, err := client.GetPublicKeyByFingerprint(exampledata.ExampleFingerprint4)

		assert.GotError(t, err)
		assert.Equal(t, ErrPublicKeyNotFound, err)
	})

	t.Run("responds with http 500 (unexpected http code)", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "")
		}
		mux.HandleFunc(
			"/key/"+exampledata.ExampleFingerprint4.Hex()+".asc",
			mockResponseHandler,
		)

		_, err := client.GetPublicKeyByFingerprint(exampledata.ExampleFingerprint4)

		assert.GotError(t, err)
		assert.Equal(t, fmt.Errorf("API error: 500"), err)
	})

	t.Run("responds with junk", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "junk body")
		}
		mux.HandleFunc(
			"/key/"+exampledata.ExampleFingerprint4.Hex()+".asc",
			mockResponseHandler,
		)

		_, err := client.GetPublicKeyByFingerprint(exampledata.ExampleFingerprint4)

		assert.GotError(t, err)
		assert.Equal(t, fmt.Errorf("failed to load armored key: error reading armored key ring: "+
			"openpgp: invalid argument: no armored data found"), err)
	})
}

func TestCreateSecret(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &v1structs.SendSecretRequest{
		RecipientFingerprint:   "OPENPGP4FPR:ABABABABABABABABABABABABABABABABABABABAB",
		ArmoredEncryptedSecret: "---- BEGIN PGP MESSAGE...",
	}

	mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
		assertClientSentVerb(t, "POST", r.Method)
		v := new(v1structs.SendSecretRequest)
		json.NewDecoder(r.Body).Decode(v)
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		w.WriteHeader(201)
	}
	mux.HandleFunc("/secrets", mockResponseHandler)

	fingerprint, err := fpr.Parse("ABAB ABAB ABAB ABAB ABAB  ABAB ABAB ABAB ABAB ABAB")
	if err != nil {
		t.Fatalf("Couldn't parse fingerprint: %s\n", err)
	}

	err = client.CreateSecret(
		fingerprint,
		"---- BEGIN PGP MESSAGE...",
	)
	assert.NoError(t, err)
}

func TestDecodeErrorResponse(t *testing.T) {
	t.Run("a response body of nil", func(t *testing.T) {
		httpResponse := http.Response{Body: nil}
		assert.Equal(t, "", decodeErrorResponse(&httpResponse))
	})
	t.Run("a response body of invalid JSON", func(t *testing.T) {
		bodyString := "foo"
		httpResponse := http.Response{
			Body: ioutil.NopCloser(strings.NewReader(bodyString)),
		}
		assert.Equal(t, "", decodeErrorResponse(&httpResponse))
	})
	t.Run("Valid JSON but missing 'detail'", func(t *testing.T) {
		bodyString := `{"foo":"bar"}`
		httpResponse := http.Response{
			Body: ioutil.NopCloser(strings.NewReader(bodyString)),
		}
		assert.Equal(t, "", decodeErrorResponse(&httpResponse))
	})
	t.Run("Valid JSON but missing 'detail'", func(t *testing.T) {
		bodyString := `{"detail":"missing record"}`
		httpResponse := http.Response{
			Body: ioutil.NopCloser(strings.NewReader(bodyString)),
		}
		assert.Equal(t, "missing record", decodeErrorResponse(&httpResponse))
	})
}

func TestUpsertTeam(t *testing.T) {
	input := &v1structs.UpsertTeamRequest{
		TeamRoster:               "# Fluidkeys team roster...",
		ArmoredDetachedSignature: "---- BEGIN PGP MESSAGE...",
	}

	fingerprint, err := fpr.Parse("ABAB ABAB ABAB ABAB ABAB  ABAB ABAB ABAB ABAB ABAB")
	if err != nil {
		t.Fatalf("Couldn't parse fingerprint: %s\n", err)
	}

	t.Run("with valid JSON response", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "POST", r.Method)
			v := new(v1structs.UpsertTeamRequest)
			json.NewDecoder(r.Body).Decode(v)
			if !reflect.DeepEqual(v, input) {
				t.Errorf("Request body = %+v, want %+v", v, input)
			}

			w.WriteHeader(201)
		}
		mux.HandleFunc("/teams", mockResponseHandler)

		err = client.UpsertTeam(
			"# Fluidkeys team roster...",
			"---- BEGIN PGP MESSAGE...",
			fingerprint,
		)
		assert.NoError(t, err)
	})

	t.Run("passes up server errors", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "POST", r.Method)
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprint(w, `{"detail": "signing key not in roster"}`)
		}
		mux.HandleFunc("/teams", mockResponseHandler)

		err = client.UpsertTeam(
			"# Fluidkeys team roster...",
			"---- BEGIN PGP MESSAGE...",
			fingerprint,
		)

		assert.Equal(t, fmt.Errorf("API error: 500 signing key not in roster"), err)
	})
}

func TestGetTeamName(t *testing.T) {
	t.Run("parses the name from a good response", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		teamUUID := uuid.Must(uuid.NewV4())
		teamResponse, err := json.Marshal(v1structs.GetTeamResponse{
			Name: "Kiffix Ltd",
		})
		if err != nil {
			t.Fatalf("failed to encode team response into JSON")
		}

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(teamResponse))
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s", teamUUID),
			mockResponseHandler,
		)

		got, err := client.GetTeamName(teamUUID)

		assert.NoError(t, err)
		assert.Equal(t, "Kiffix Ltd", got)
	})

	t.Run("404 returns a specific type of error", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		unknownUUID := uuid.Must(uuid.NewV4())

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s", unknownUUID),
			mockResponseHandler,
		)

		_, err := client.GetTeamName(unknownUUID)

		assert.Equal(t, ErrTeamNotFound, err)
	})

	t.Run("responds with http 500 (unexpected http code)", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		teamUUID := uuid.Must(uuid.NewV4())

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s", teamUUID),
			mockResponseHandler,
		)

		_, err := client.GetTeamName(teamUUID)

		assert.GotError(t, err)
		assert.Equal(t, fmt.Errorf("API error: 500"), err)
	})
}

func TestGetTeamRoster(t *testing.T) {
	teamUUID := uuid.Must(uuid.NewV4())

	requesterKey, err := pgpkey.LoadFromArmoredEncryptedPrivateKey(
		exampledata.ExamplePrivateKey4, "test4",
	)
	assert.NoError(t, err)

	expectedRoster := "fake roster"
	expectedSignature := "fake signature"

	decryptedRosterAndSig := v1structs.TeamRosterAndSignature{
		TeamRoster:               expectedRoster,
		ArmoredDetachedSignature: expectedSignature,
	}

	jsonRosterAndSig, err := json.MarshalIndent(decryptedRosterAndSig, "", "    ")
	assert.NoError(t, err)

	encryptedJSON, err := encryptToArmor(t, string(jsonRosterAndSig), requesterKey)
	assert.NoError(t, err)

	client, mux, _, teardown := setup()
	defer teardown()

	teamRosterResponse, err := json.Marshal(v1structs.GetTeamRosterResponse{
		EncryptedJSON: encryptedJSON,
	})
	assert.NoError(t, err)

	t.Run("decrypts the roster with a valid requester key", func(t *testing.T) {
		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentValidAuthHeader(t, requesterKey.Fingerprint(), r.Header)
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(teamRosterResponse))
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/roster", teamUUID),
			mockResponseHandler,
		)

		gotRoster, gotSignature, err := client.GetTeamRoster(*requesterKey, teamUUID)

		assert.NoError(t, err)
		assert.Equal(t, expectedRoster, gotRoster)
		assert.Equal(t, expectedSignature, gotSignature)
	})

	t.Run("returns an error given an invalid requester key", func(t *testing.T) {
		invalidKeyTeamUUID := uuid.Must(uuid.NewV4())
		invalidRequesterKey, err := pgpkey.LoadFromArmoredEncryptedPrivateKey(
			exampledata.ExamplePrivateKey2, "test2",
		)
		assert.NoError(t, err)

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentValidAuthHeader(t, invalidRequesterKey.Fingerprint(), r.Header)
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(teamRosterResponse))
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/roster", invalidKeyTeamUUID),
			mockResponseHandler,
		)

		_, _, err = client.GetTeamRoster(*invalidRequesterKey, invalidKeyTeamUUID)

		assert.GotError(t, err)
		assert.Equal(t,
			fmt.Errorf("couldn't decrypt json: error reading message: openpgp: incorrect key"), err,
		)
	})

	t.Run("404 returns a specific type of error", func(t *testing.T) {
		unknownUUID := uuid.Must(uuid.NewV4())
		mockNotFoundResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/roster", unknownUUID),
			mockNotFoundResponseHandler,
		)

		_, _, err := client.GetTeamRoster(*requesterKey, unknownUUID)

		assert.Equal(t, ErrTeamNotFound, err)
	})

	t.Run("403 forbidden returns ErrForbidden", func(t *testing.T) {
		teamUUID := uuid.Must(uuid.NewV4())
		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/roster", teamUUID),
			mockResponseHandler,
		)

		_, _, err := client.GetTeamRoster(*requesterKey, teamUUID)

		assert.Equal(t, ErrForbidden, err)
	})

	t.Run("responds with http 500 (unexpected http code)", func(t *testing.T) {
		errorUUID := uuid.Must(uuid.NewV4())
		mockErrorResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/roster", errorUUID),
			mockErrorResponseHandler,
		)

		_, _, err := client.GetTeamRoster(*requesterKey, errorUUID)

		assert.GotError(t, err)
		assert.Equal(t, fmt.Errorf("API error: 500"), err)
	})
}

func TestRequestToJoinTeam(t *testing.T) {
	expectedRequest := &v1structs.RequestToJoinTeamRequest{TeamEmail: "jane@example.com"}
	fingerprint, err := fpr.Parse("ABAB ABAB ABAB ABAB ABAB  ABAB ABAB ABAB ABAB ABAB")
	if err != nil {
		t.Fatalf("Couldn't parse fingerprint: %s\n", err)
	}
	mockTeamUUID := uuid.Must(uuid.NewV4())

	t.Run("with valid JSON response", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "POST", r.Method)
			gotRequest := new(v1structs.RequestToJoinTeamRequest)
			json.NewDecoder(r.Body).Decode(gotRequest)
			assert.Equal(t, expectedRequest, gotRequest)
			w.WriteHeader(http.StatusCreated)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/requests-to-join", mockTeamUUID),
			mockResponseHandler,
		)

		err = client.RequestToJoinTeam(
			mockTeamUUID,
			fingerprint,
			"jane@example.com",
		)
		assert.NoError(t, err)
	})

	t.Run("with a conflicting response status", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "POST", r.Method)
			gotRequest := new(v1structs.RequestToJoinTeamRequest)
			json.NewDecoder(r.Body).Decode(gotRequest)
			assert.Equal(t, expectedRequest, gotRequest)
			w.WriteHeader(http.StatusConflict)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/requests-to-join", mockTeamUUID),
			mockResponseHandler,
		)

		err = client.RequestToJoinTeam(
			mockTeamUUID,
			fingerprint,
			"jane@example.com",
		)
		assert.Equal(t, fmt.Errorf("already got request to join team for jane@example.com"), err)
	})

	t.Run("passes up server errors", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "POST", r.Method)
			gotRequest := new(v1structs.RequestToJoinTeamRequest)
			json.NewDecoder(r.Body).Decode(gotRequest)
			assert.Equal(t, expectedRequest, gotRequest)

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"detail": "can't write to database"}`)
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/requests-to-join", mockTeamUUID),
			mockResponseHandler,
		)

		err = client.RequestToJoinTeam(
			mockTeamUUID,
			fingerprint,
			"jane@example.com",
		)
		assert.Equal(t, fmt.Errorf("API error: 500 can't write to database"), err)
	})
}

func TestListRequestsToJoinTeam(t *testing.T) {
	authFingerprint, err := fpr.Parse("ABABABABABABABABABABABABABABABABABABABAB")
	if err != nil {
		t.Fatalf("Couldn't parse fingerprint: %s\n", err)
	}
	teamUUID := uuid.Must(uuid.NewV4())

	t.Run("responds with a list of good requests", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		expectedRequestsToJoin := []team.RequestToJoinTeam{
			{
				UUID:        uuid.Must(uuid.FromString("8e26e4df0d474f7f9a07a37b2aa92104")),
				TeamUUID:    teamUUID,
				Email:       "first@example.com",
				Fingerprint: fpr.MustParse("AAAABBBBAAAABBBBAAAAAAAABBBBAAAABBBBAAAA"),
				RequestedAt: time.Time{},
			},
			{
				UUID:        uuid.Must(uuid.FromString("a57dbf76c2f04bbd9a334cba1b7e335c")),
				TeamUUID:    teamUUID,
				Email:       "second@example.com",
				Fingerprint: fpr.MustParse("CCCCDDDDCCCCDDDDCCCCDDDDCCCCDDDDCCCCDDDD"),
				RequestedAt: time.Time{},
			},
		}
		joinTeamRequestsResponse, err := json.Marshal(
			v1structs.ListRequestsToJoinTeamResponse{
				Requests: []v1structs.RequestToJoinTeam{
					{
						UUID:        "8e26e4df0d474f7f9a07a37b2aa92104",
						Email:       "first@example.com",
						Fingerprint: "OPENPGP4FPR:AAAABBBBAAAABBBBAAAAAAAABBBBAAAABBBBAAAA",
					},
					{
						UUID:        "a57dbf76c2f04bbd9a334cba1b7e335c",
						Email:       "second@example.com",
						Fingerprint: "OPENPGP4FPR:CCCCDDDDCCCCDDDDCCCCDDDDCCCCDDDDCCCCDDDD",
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("failed to encode join team requests into JSON: %v", err)
		}

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(joinTeamRequestsResponse))
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/requests-to-join", teamUUID),
			mockResponseHandler,
		)

		got, err := client.ListRequestsToJoinTeam(teamUUID, authFingerprint)

		assert.NoError(t, err)
		assert.Equal(t, expectedRequestsToJoin, got)
	})

	t.Run("drops any requests with invalid uuids", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		expectedRequestsToJoin := []team.RequestToJoinTeam{
			{
				UUID:        uuid.Must(uuid.FromString("8e26e4df0d474f7f9a07a37b2aa92104")),
				TeamUUID:    teamUUID,
				Email:       "first@example.com",
				Fingerprint: fpr.MustParse("AAAABBBBAAAABBBBAAAAAAAABBBBAAAABBBBAAAA"),
			},
		}

		joinTeamRequestsResponse, err := json.Marshal(
			v1structs.ListRequestsToJoinTeamResponse{
				Requests: []v1structs.RequestToJoinTeam{
					{
						UUID:        "8e26e4df0d474f7f9a07a37b2aa92104",
						Email:       "first@example.com",
						Fingerprint: "OPENPGP4FPR:AAAABBBBAAAABBBBAAAAAAAABBBBAAAABBBBAAAA",
					},
					{
						UUID:        "invalid-uuid",
						Email:       "second@example.com",
						Fingerprint: "OPENPGP4FPR:CCCCDDDDCCCCDDDDCCCCDDDDCCCCDDDDCCCCDDDD",
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("failed to encode join team requests into JSON: %v", err)
		}

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(joinTeamRequestsResponse))
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/requests-to-join", teamUUID),
			mockResponseHandler,
		)

		got, err := client.ListRequestsToJoinTeam(teamUUID, authFingerprint)

		assert.NoError(t, err)
		assert.Equal(t, expectedRequestsToJoin, got)
	})

	t.Run("drops any requests with invalid fingerprints", func(t *testing.T) {
		client, mux, _, teardown := setup()
		defer teardown()

		expectedRequestsToJoin := []team.RequestToJoinTeam{
			{
				UUID:        uuid.Must(uuid.FromString("8e26e4df0d474f7f9a07a37b2aa92104")),
				TeamUUID:    teamUUID,
				Email:       "first@example.com",
				Fingerprint: fpr.MustParse("AAAABBBBAAAABBBBAAAAAAAABBBBAAAABBBBAAAA"),
			},
		}

		joinTeamRequestsResponse, err := json.Marshal(
			v1structs.ListRequestsToJoinTeamResponse{
				Requests: []v1structs.RequestToJoinTeam{
					{
						UUID:        "8e26e4df0d474f7f9a07a37b2aa92104",
						Email:       "first@example.com",
						Fingerprint: "OPENPGP4FPR:AAAABBBBAAAABBBBAAAAAAAABBBBAAAABBBBAAAA",
					},
					{
						UUID:        "a57dbf76c2f04bbd9a334cba1b7e335c",
						Email:       "second@example.com",
						Fingerprint: "invalid-fingerprint",
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("failed to encode join team requests into JSON: %v", err)
		}

		mockResponseHandler := func(w http.ResponseWriter, r *http.Request) {
			assertClientSentVerb(t, "GET", r.Method)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(joinTeamRequestsResponse))
		}
		mux.HandleFunc(
			fmt.Sprintf("/team/%s/requests-to-join", teamUUID),
			mockResponseHandler,
		)

		got, err := client.ListRequestsToJoinTeam(teamUUID, authFingerprint)

		assert.NoError(t, err)
		assert.Equal(t, expectedRequestsToJoin, got)
	})
}

// setup sets up a test HTTP server along with a fluidkeysServer.Client that is
// configured to talk to that test server. Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func setup() (client *Client, mux *http.ServeMux, serverURL string, teardown func()) {
	// mux is the HTTP request multiplexer used with the test server.
	mux = http.NewServeMux()
	apiHandler := http.NewServeMux()
	apiHandler.Handle("/", mux)

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(apiHandler)

	// client is the Fluidkeys Server client being tested and is
	// configured to use test server.
	client = NewClient("vtest")
	url, _ := url.Parse(server.URL + "/")
	client.BaseURL = url

	return client, mux, server.URL, server.Close
}

func assertClientSentVerb(t *testing.T, expectedVerb string, gotVerb string) {
	if gotVerb != expectedVerb {
		t.Errorf("Expected request verb: %s, got %s", expectedVerb, gotVerb)
	}
}

func assertClientSentValidAuthHeader(t *testing.T, expectedFingerprint fpr.Fingerprint, gotHeader http.Header) {
	expectedAuthorization := authorization(expectedFingerprint)
	if gotHeader.Get("authorization") != expectedAuthorization {
		t.Errorf(
			"Expected authorization: %s, got %s",
			expectedAuthorization, gotHeader.Get("authorization"),
		)
	}
}

func encryptToArmor(t *testing.T, decryptedMessage string, pgpKey *pgpkey.PgpKey) (string, error) {
	t.Helper()

	buffer := bytes.NewBuffer(nil)
	message, err := armor.Encode(buffer, "PGP MESSAGE", nil)
	if err != nil {
		return "", err
	}
	pgpWriteCloser, err := openpgp.Encrypt(
		message,
		[]*openpgp.Entity{&pgpKey.Entity},
		nil,
		nil,
		nil,
	)
	if err != nil {
		return "", err
	}
	_, err = pgpWriteCloser.Write([]byte(decryptedMessage))
	if err != nil {
		return "", err
	}
	pgpWriteCloser.Close()
	message.Close()
	return buffer.String(), nil
}
