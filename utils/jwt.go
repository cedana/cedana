package utils

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// JWTs are used for authenticating the client/server conn
// We're in a unique situation where the clients are more or less generated
// by the server, so we get to avoid a lot of the headaches associated w/
// getting a JWT on a client in the first place.

func GenerateJWT(id string, skey string) (*string, error) {
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		Issuer:    "ced-orch",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	ss, err := token.SignedString([]byte(skey))
	if err != nil {
		return nil, err
	}
	return &ss, nil
}
