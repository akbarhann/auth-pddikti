package pddikti

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by Client. Use errors.Is to classify them.
var (
	ErrNotFound  = errors.New("mahasiswa tidak ditemukan di PDDIKTI")
	ErrAmbiguous = errors.New("NIM cocok dengan lebih dari satu perguruan tinggi")
)

// Campus identifies one candidate campus when a NIM is ambiguous.
// It intentionally carries no personal data.
type Campus struct {
	NamaPT      string `json:"nama_pt"`
	SingkatanPT string `json:"singkatan_pt"`
	NamaProdi   string `json:"nama_prodi"`
}

// AmbiguousError is returned when one NIM exactly matches students at more
// than one campus and no campus filter narrowed it down to one.
type AmbiguousError struct {
	NIM        string
	Candidates []Campus
}

func (e *AmbiguousError) Error() string {
	names := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		names[i] = c.NamaPT
	}
	return fmt.Sprintf("%s: NIM '%s' terdaftar di %s", ErrAmbiguous, e.NIM, strings.Join(names, ", "))
}

func (e *AmbiguousError) Is(target error) bool { return target == ErrAmbiguous }
