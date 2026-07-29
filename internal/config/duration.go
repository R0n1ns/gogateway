package config

import (
	"encoding/json"
	"errors"
	"time"
)

// Duration хранит time.Duration и сериализуется как понятная человеку строка.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("длительность должна быть строкой")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	if parsed <= 0 {
		return errors.New("длительность должна быть больше нуля")
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }
