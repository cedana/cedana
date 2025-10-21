package utils

import (
	"encoding/json"
	"io"
)

type HttpBody struct {
	Message string `json:"message"`
}

func ParseHttpBody(body io.ReadCloser) (string, error) {
	defer body.Close()
	var httpBody HttpBody

	err := json.NewDecoder(body).Decode(&httpBody)
	if err != nil {
		return "", err
	}

	return httpBody.Message, nil
}
