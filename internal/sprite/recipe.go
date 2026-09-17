package sprite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func ParseRecipe(data []byte) (Recipe, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var document struct {
		Version int             `json:"version"`
		Grid    json.RawMessage `json:"grid"`
		Colors  int             `json:"colors"`
		Method  string          `json:"method"`
		Scale   int             `json:"scale"`
	}
	if err := dec.Decode(&document); err != nil {
		return Recipe{}, fmt.Errorf("parse recipe: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Recipe{}, fmt.Errorf("parse recipe: multiple JSON values")
		}
		return Recipe{}, fmt.Errorf("parse recipe: %w", err)
	}
	grid := 0
	if len(document.Grid) > 0 {
		var name string
		if err := json.Unmarshal(document.Grid, &name); err == nil {
			if !strings.EqualFold(strings.TrimSpace(name), "auto") {
				return Recipe{}, fmt.Errorf("parse recipe: grid must be auto or an integer")
			}
		} else if err := json.Unmarshal(document.Grid, &grid); err != nil {
			return Recipe{}, fmt.Errorf("parse recipe: grid must be auto or an integer: %w", err)
		}
	}
	recipe := Recipe{
		Version: document.Version,
		Grid:    grid,
		Colors:  document.Colors,
		Method:  document.Method,
		Scale:   document.Scale,
	}
	normalized, err := recipe.Normalize()
	if err != nil {
		return Recipe{}, err
	}
	return normalized, nil
}

func MarshalRecipe(recipe Recipe) ([]byte, error) {
	normalized, err := recipe.Normalize()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode recipe: %w", err)
	}
	return append(data, '\n'), nil
}
