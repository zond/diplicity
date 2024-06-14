package auth2

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
)

func decodeFromBase64(input string, obj interface{}) error {
	b, err := base64.URLEncoding.DecodeString(input)
	if err != nil {
		return err
	}
	return gob.NewDecoder(bytes.NewReader(b)).Decode(obj)
}

func encodeToBase64(obj interface{}) (string, error) {
	buf := bytes.Buffer{}
	if err := gob.NewEncoder(&buf).Encode(obj); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf.Bytes()), nil
}
