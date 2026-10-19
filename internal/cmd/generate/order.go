package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// pointer is a place in a JSON document, one key or index at a time.
type pointer []string

func (p pointer) to(keys ...string) pointer { return append(append(pointer{}, p...), keys...) }
func (p pointer) String() string            { return "/" + strings.Join(p, "\x00") }

// keyOrder is the order of every object's keys in a JSON document, by the object's place. Go's maps
// forget it, and the spec's order matters twice: docs.haau3.com numbers repeated page names in it,
// and the first of an operation's examples is the one each SDK's tests read.
type keyOrder map[string][]string

func readKeyOrder(data []byte) (keyOrder, error) {
	order := keyOrder{}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := order.walk(dec, pointer{}); err != nil {
		return nil, err
	}
	return order, nil
}

func (order keyOrder) walk(dec *json.Decoder, here pointer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		var keys []string
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			k := key.(string)
			keys = append(keys, k)
			if err := order.walk(dec, here.to(k)); err != nil {
				return err
			}
		}
		order[here.String()] = keys
		_, err = dec.Token()
		return err
	case json.Delim('['):
		for i := 0; dec.More(); i++ {
			if err := order.walk(dec, here.to(fmt.Sprint(i))); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	return nil
}

// keys is m's keys in the document's order. A key the document does not hold is an error in the
// generator, so it panics rather than guess.
func (order keyOrder) keys(at pointer, m map[string]any) []string {
	keys := order[at.String()]
	if len(keys) != len(m) {
		panic(fmt.Sprintf("the key order at %v has %d keys for an object of %d", at, len(keys), len(m)))
	}
	return keys
}
