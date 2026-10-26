package main

import (
	"bytes"
	"encoding/base64"
	"errors"
)

// FlixCloud's HLS response is a Base64-encoded playlist XOR'd with the key
// produced by its player WASM. The stream-info result carries that key.
func flixDecodeManifest(body []byte, encodedKey string) ([]byte, error) {
	body = bytes.TrimSpace(body)
	if bytes.HasPrefix(body, []byte("#EXTM3U")) {
		return bytes.Clone(body), nil
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid FlixCloud playlist key")
	}
	out, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil || len(out) == 0 || len(out) > 4<<20 {
		return nil, errors.New("invalid FlixCloud playlist encoding")
	}
	for i := range out {
		out[i] ^= key[i%len(key)]
	}
	if !bytes.HasPrefix(out, []byte("#EXTM3U")) {
		return nil, errors.New("FlixCloud playlist decoding failed")
	}
	return out, nil
}

var flixSegmentMask = [...]byte{157, 42, 241, 71, 179, 142, 92, 112, 166, 25, 228, 59, 216, 98, 15}

// Some FlixCloud transport streams are delivered inside WEBP or PNG wrappers,
// with a second XOR applied to their payload. Don't alter ordinary segments.
func flixDecodeSegment(body []byte) ([]byte, error) {
	payload := body
	switch {
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		payload = bytes.Clone(body[12:])
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		payload = bytes.Clone(body[8:])
	default:
		return body, nil
	}
	if len(payload) == 0 {
		return nil, errors.New("empty FlixCloud segment")
	}
	if payload[0] == 0x47 || len(payload) > 8 && string(payload[4:8]) == "ftyp" {
		return payload, nil
	}
	for i := range payload {
		payload[i] ^= flixSegmentMask[i%len(flixSegmentMask)]
	}
	if payload[0] != 0x47 && !(len(payload) > 8 && string(payload[4:8]) == "ftyp") {
		return nil, errors.New("FlixCloud segment decoding failed")
	}
	return payload, nil
}
