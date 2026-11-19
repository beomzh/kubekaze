// Package ui contains small interactive helpers.
package ui

import (
	"errors"

	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
)

// ErrCancelled is returned when the user aborts a selection.
var ErrCancelled = errors.New("cancelled")

// Pick shows a fuzzy-searchable list and returns the chosen item.
func Pick(items []string, prompt string) (string, error) {
	if len(items) == 0 {
		return "", errors.New("nothing to choose from")
	}
	idx, err := fuzzyfinder.Find(items, func(i int) string { return items[i] },
		fuzzyfinder.WithPromptString(prompt+"> "))
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return "", ErrCancelled
		}
		return "", err
	}
	return items[idx], nil
}

// PickMany lets the user select several items (Tab to mark, Enter to confirm).
func PickMany(items []string, prompt string) ([]string, error) {
	if len(items) == 0 {
		return nil, errors.New("nothing to choose from")
	}
	idxs, err := fuzzyfinder.FindMulti(items, func(i int) string { return items[i] },
		fuzzyfinder.WithPromptString(prompt+"> "))
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return nil, ErrCancelled
		}
		return nil, err
	}
	out := make([]string, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, items[i])
	}
	return out, nil
}
