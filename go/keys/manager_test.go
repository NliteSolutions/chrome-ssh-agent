// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package keys

import (
	"errors"
	"testing"

	"github.com/google/chrome-ssh-agent/go/chrome/fakes"
	"github.com/google/chrome-ssh-agent/go/keys/testdata"
	"github.com/kr/pretty"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type initialKey struct {
	Name          string
	PEMPrivateKey string
}

func newTestManager(agent agent.Agent, storage PersistentStore, keys []*initialKey) (Manager, error) {
	mgr := NewManager(agent, storage)
	for _, k := range keys {
		if err := syncAdd(mgr, k.Name, k.PEMPrivateKey); err != nil {
			return nil, err
		}
	}

	return mgr, nil
}

func TestAdd(t *testing.T) {
	testcases := []struct {
		description    string
		initial        []*initialKey
		name           string
		pemPrivateKey  string
		storageErr     fakes.Errs
		wantConfigured []string
		wantErr        error
	}{
		{
			description:    "add single key",
			name:           "new-key",
			pemPrivateKey:  testdata.ValidPrivateKey,
			wantConfigured: []string{"new-key"},
		},
		{
			description: "add multiple keys",
			initial: []*initialKey{
				{
					Name:          "new-key-1",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			name:           "new-key-2",
			pemPrivateKey:  testdata.ValidPrivateKey,
			wantConfigured: []string{"new-key-1", "new-key-2"},
		},
		{
			description: "add multiple keys with same name",
			initial: []*initialKey{
				{
					Name:          "new-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			name:           "new-key",
			pemPrivateKey:  testdata.ValidPrivateKey,
			wantConfigured: []string{"new-key", "new-key"},
		},
		{
			description:   "reject invalid name",
			name:          "",
			pemPrivateKey: testdata.ValidPrivateKey,
			wantErr:       errors.New("name must not be empty"),
		},
		{
			description:   "fail to write to storage",
			name:          "new-key",
			pemPrivateKey: testdata.ValidPrivateKey,
			storageErr: fakes.Errs{
				Set: errors.New("storage.Set failed"),
			},
			wantErr: errors.New("storage.Set failed"),
		},
	}

	for _, tc := range testcases {
		storage := fakes.NewMemStorage()
		mgr, err := newTestManager(agent.NewKeyring(), storage, tc.initial)
		if err != nil {
			t.Fatalf("%s: failed to initialize manager: %v", tc.description, err)
		}

		// Add the key.
		func() {
			storage.SetError(tc.storageErr)
			defer storage.SetError(fakes.Errs{})

			err := syncAdd(mgr, tc.name, tc.pemPrivateKey)
			if diff := pretty.Diff(err, tc.wantErr); diff != nil {
				t.Errorf("%s: incorrect error; -got +want: %s", tc.description, diff)
			}
		}()

		// Ensure the correct keys are configured at the end.
		configured, err := syncConfigured(mgr)
		if err != nil {
			t.Errorf("%s: failed to get configured keys: %v", tc.description, err)
		}
		names := configuredKeyNames(configured)
		if diff := pretty.Diff(names, tc.wantConfigured); diff != nil {
			t.Errorf("%s: incorrect configured keys; -got +want: %s", tc.description, diff)
		}
	}
}

func TestRemove(t *testing.T) {
	testcases := []struct {
		description    string
		initial        []*initialKey
		byName         string
		byID           ID
		storageErr     fakes.Errs
		wantConfigured []string
		wantErr        error
	}{
		{
			description: "remove single key",
			initial: []*initialKey{
				{
					Name:          "new-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName:         "new-key",
			wantConfigured: nil,
		},
		{
			description: "fail to remove key with invalid ID",
			initial: []*initialKey{
				{
					Name:          "new-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byID:           ID("bogus-id"),
			wantConfigured: []string{"new-key"},
		},
		{
			description: "fail to read from storage",
			initial: []*initialKey{
				{
					Name:          "new-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName: "new-key",
			storageErr: fakes.Errs{
				Get: errors.New("storage.Get failed"),
			},
			wantConfigured: []string{"new-key"},
			wantErr:        errors.New("failed to enumerate keys: failed to read from storage: storage.Get failed"),
		},
		{
			description: "fail to write to storage",
			initial: []*initialKey{
				{
					Name:          "new-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName: "new-key",
			storageErr: fakes.Errs{
				Delete: errors.New("storage.Delete failed"),
			},
			wantConfigured: []string{"new-key"},
			wantErr:        errors.New("failed to delete keys: storage.Delete failed"),
		},
	}

	for _, tc := range testcases {
		storage := fakes.NewMemStorage()
		mgr, err := newTestManager(agent.NewKeyring(), storage, tc.initial)
		if err != nil {
			t.Fatalf("%s: failed to initialize manager: %v", tc.description, err)
		}

		// Figure out the ID of the key we will try to remove.
		id, err := findKey(mgr, tc.byID, tc.byName)
		if err != nil {
			t.Fatalf("%s: failed to find key: %v", tc.description, err)
		}

		// Remove the key
		func() {
			storage.SetError(tc.storageErr)
			defer storage.SetError(fakes.Errs{})

			err := syncRemove(mgr, id)
			if diff := pretty.Diff(err, tc.wantErr); diff != nil {
				t.Errorf("%s: incorrect error; -got +want: %s", tc.description, diff)
			}
		}()

		// Ensure the correct keys are configured at the end.
		configured, err := syncConfigured(mgr)
		if err != nil {
			t.Errorf("%s: failed to get configured keys: %v", tc.description, err)
		}
		names := configuredKeyNames(configured)
		if diff := pretty.Diff(names, tc.wantConfigured); diff != nil {
			t.Errorf("%s: incorrect configured keys; -got +want: %s", tc.description, diff)
		}
	}
}

func TestConfigured(t *testing.T) {
	testcases := []struct {
		description    string
		initial        []*initialKey
		storageErr     fakes.Errs
		wantConfigured []string
		wantErr        error
	}{
		{
			description: "empty list on no keys",
		},
		{
			description: "enumerate multiple keys",
			initial: []*initialKey{
				{
					Name:          "new-key-1",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
				{
					Name:          "new-key-2",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			wantConfigured: []string{"new-key-1", "new-key-2"},
		},
		{
			description: "fail to read from storage",
			initial: []*initialKey{
				{
					Name:          "new-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			storageErr: fakes.Errs{
				Get: errors.New("storage.Get failed"),
			},
			wantErr: errors.New("failed to read keys: failed to read from storage: storage.Get failed"),
		},
	}

	for _, tc := range testcases {
		storage := fakes.NewMemStorage()
		mgr, err := newTestManager(agent.NewKeyring(), storage, tc.initial)
		if err != nil {
			t.Fatalf("%s: failed to initialize manager: %v", tc.description, err)
		}

		// Enumerate the keys.
		func() {
			storage.SetError(tc.storageErr)
			defer storage.SetError(fakes.Errs{})

			configured, err := syncConfigured(mgr)
			if diff := pretty.Diff(err, tc.wantErr); diff != nil {
				t.Errorf("%s: incorrect error; -got +want: %s", tc.description, diff)
			}
			names := configuredKeyNames(configured)
			if diff := pretty.Diff(names, tc.wantConfigured); diff != nil {
				t.Errorf("%s: incorrect configured keys; -got +want: %s", tc.description, diff)
			}
		}()
	}
}

func TestLoadAndLoaded(t *testing.T) {
	testcases := []struct {
		description string
		initial     []*initialKey
		byName      string
		byID        ID
		passphrase  string
		storageErr  fakes.Errs
		wantLoaded  []string
		wantErr     error
	}{
		{
			description: "load single key",
			initial: []*initialKey{
				{
					Name:          "good-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName:     "good-key",
			passphrase: testdata.ValidPrivateKeyPassphrase,
			wantLoaded: []string{
				testdata.ValidPrivateKeyBlob,
			},
		},
		{
			description: "load one of multiple keys",
			initial: []*initialKey{
				{
					Name:          "bad-key",
					PEMPrivateKey: "bogus-key-data",
				},
				{
					Name:          "good-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName:     "good-key",
			passphrase: testdata.ValidPrivateKeyPassphrase,
			wantLoaded: []string{
				testdata.ValidPrivateKeyBlob,
			},
		},
		{
			description: "fail on invalid private key",
			initial: []*initialKey{
				{
					Name:          "bad-key",
					PEMPrivateKey: "bogus-key-data",
				},
			},
			byName:     "bad-key",
			passphrase: "some passphrase",
			wantErr:    errors.New("failed to parse private key: ssh: no key found"),
		},
		{
			description: "fail on invalid password",
			initial: []*initialKey{
				{
					Name:          "good-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName:     "good-key",
			passphrase: "incorrect passphrase",
			wantErr:    errors.New("failed to parse private key: x509: decryption password incorrect"),
		},
		{
			description: "fail on invalid ID",
			initial: []*initialKey{
				{
					Name:          "good-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byID:       ID("bogus-id"),
			passphrase: "some passphrase",
			wantErr:    errors.New("failed to find key with ID bogus-id"),
		},
		{
			description: "fail to read from storage",
			initial: []*initialKey{
				{
					Name:          "good-key",
					PEMPrivateKey: testdata.ValidPrivateKey,
				},
			},
			byName:     "good-key",
			passphrase: testdata.ValidPrivateKeyPassphrase,
			storageErr: fakes.Errs{
				Get: errors.New("storage.Get failed"),
			},
			wantErr: errors.New("failed to read key: failed to read keys: failed to read from storage: storage.Get failed"),
		},
	}

	for _, tc := range testcases {
		storage := fakes.NewMemStorage()
		mgr, err := newTestManager(agent.NewKeyring(), storage, tc.initial)
		if err != nil {
			t.Fatalf("%s: failed to initialize manager: %v", tc.description, err)
		}

		// Figure out the ID of the key we will try to load.
		id, err := findKey(mgr, tc.byID, tc.byName)
		if err != nil {
			t.Fatalf("%s: failed to find key: %v", tc.description, err)
		}

		// Load the key
		func() {
			storage.SetError(tc.storageErr)
			defer storage.SetError(fakes.Errs{})

			err := syncLoad(mgr, id, tc.passphrase)
			if diff := pretty.Diff(err, tc.wantErr); diff != nil {
				t.Errorf("%s: incorrect error; -got +want: %s", tc.description, diff)
			}
		}()

		// Ensure the correct keys are loaded at the end.
		loaded, err := syncLoaded(mgr)
		if err != nil {
			t.Errorf("%s: failed to get loaded keys: %v", tc.description, err)
		}
		blobs := loadedKeyBlobs(loaded)
		if diff := pretty.Diff(blobs, tc.wantLoaded); diff != nil {
			t.Errorf("%s: incorrect loaded keys; -got +want: %s", tc.description, diff)
		}
	}
}

func TestGetID(t *testing.T) {
	// Create a manager with one configured key.  We load the key and
	// ensure we can correctly extract the ID.
	storage := fakes.NewMemStorage()
	agt := agent.NewKeyring()
	mgr, err := newTestManager(agt, storage, []*initialKey{
		{
			Name:          "good-key",
			PEMPrivateKey: testdata.ValidPrivateKey,
		},
	})
	if err != nil {
		t.Fatalf("failed to initialize manager: %v", err)
	}

	// Locate the ID corresponding to the key we configured.
	wantID, err := findKey(mgr, InvalidID, "good-key")
	if err != nil {
		t.Errorf("failed to find ID for good-key: %v", err)
	}

	// Load the key.
	if err := syncLoad(mgr, wantID, testdata.ValidPrivateKeyPassphrase); err != nil {
		t.Errorf("failed to load key: %v", err)
	}

	// Ensure that we can correctly read the ID from the key we loaded.
	loaded, err := syncLoaded(mgr)
	if err != nil {
		t.Errorf("failed to enumerate loaded keys: %v", err)
	}
	if diff := pretty.Diff(loadedKeyIds(loaded), []ID{wantID}); diff != nil {
		t.Errorf("incorrect loaded key IDs; -got +want: %s", diff)
	}

	// Now, also load a key into the agent directly (i.e., not through the
	// manager). We will ensure that we get InvalidID back when we try
	// to extract the ID from it.
	priv, err := ssh.ParseRawPrivateKey([]byte(testdata.ValidPrivateKeyWithoutPassphrase))
	if err != nil {
		t.Errorf("failed to parse private key: %v", err)
	}
	err = agt.Add(agent.AddedKey{
		PrivateKey: priv,
		Comment:    "some comment",
	})
	if err != nil {
		t.Errorf("failed to load key into agent: %v", err)
	}
	loaded, err = syncLoaded(mgr)
	if err != nil {
		t.Errorf("failed to enumerate loaded keys: %v", err)
	}
	if diff := pretty.Diff(loadedKeyIds(loaded), []ID{wantID, InvalidID}); diff != nil {
		t.Errorf("incorrect loaded key IDs; -got +want: %s", diff)
	}
}
