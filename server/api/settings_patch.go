package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func settingsETag(s AppSettings) string {
	b, _ := json.Marshal(s)
	h := sha256.Sum256(b)
	return `"` + hex.EncodeToString(h[:]) + `"`
}

// Only supplied leaf fields are merged; omission is not false/zero.
func mergeSettings(current AppSettings, body []byte) (AppSettings, error) {
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(body, &patch); err != nil || patch == nil {
		return current, fmt.Errorf("settings patch must be an object")
	}
	b, _ := json.Marshal(current)
	var base map[string]json.RawMessage
	_ = json.Unmarshal(b, &base)
	for k, v := range patch {
		if _, ok := base[k]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return current, fmt.Errorf("invalid settings field: %s", k)
		}
		if k == "ai" {
			var inner, old map[string]json.RawMessage
			if err := json.Unmarshal(v, &inner); err != nil || inner == nil {
				return current, fmt.Errorf("ai must be an object")
			}
			_ = json.Unmarshal(base[k], &old)
			for ik, iv := range inner {
				if _, ok := old[ik]; !ok || bytes.Equal(bytes.TrimSpace(iv), []byte("null")) {
					return current, fmt.Errorf("invalid ai field: %s", ik)
				}
				old[ik] = iv
			}
			base[k], _ = json.Marshal(old)
		} else {
			base[k] = v
		}
	}
	b, _ = json.Marshal(base)
	var out AppSettings
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return current, err
	}
	return out, nil
}

func readSettingsBody(r io.Reader) ([]byte, error) { return io.ReadAll(io.LimitReader(r, 1<<20)) }

// Write before publishing the in-memory state. A failed disk write must not be success.
func (s *Server) persistSettingsValue(value AppSettings) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.settingsPath()), ".settings-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name) // verified temporary file created above
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.settingsPath())
}
