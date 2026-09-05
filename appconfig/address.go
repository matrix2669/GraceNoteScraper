package appconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Private address storage is intentionally separate from public setup config.
type addressRecord struct {
	Fingerprint string          `json:"fingerprint"`
	Address     json.RawMessage `json:"address"`
}

func (s *Store) addressPath() string { return s.path + ".address.json" }

func (s *Store) Address(fingerprint string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.configured || s.config.Fingerprint() != fingerprint {
		return nil, errors.New("provider changed; reload the page")
	}
	data, err := os.ReadFile(s.addressPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("unable to read saved service address")
	}
	var record addressRecord
	if json.Unmarshal(data, &record) != nil {
		return nil, errors.New("saved service address is invalid; select it again")
	}
	if record.Fingerprint != fingerprint {
		return nil, nil
	}
	return record.Address, nil
}

func (s *Store) SaveAddress(fingerprint string, address json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configured || s.config.Fingerprint() != fingerprint {
		return errors.New("provider changed; reload the page")
	}
	if len(address) == 0 {
		err := os.Remove(s.addressPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := json.Marshal(addressRecord{fingerprint, address})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".provider-address-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), s.addressPath())
}
