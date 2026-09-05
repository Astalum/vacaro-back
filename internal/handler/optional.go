package handler

import (
	"bytes"
	"encoding/json"
)

type optional[T any] struct {
	Value T
	Set   bool
	Null  bool
}

func (o *optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(data, []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}
