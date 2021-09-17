package urlsigner

import (
	"fmt"
	"strings"
	"time"

	goalone "github.com/bwmarrin/go-alone"
)

type Signer struct {
	Secret []byte
}

func (s *Signer) GenerateTokenFromString(data string) string {
	var urlToSign string

	// create a new signer using secret & timestamp
	crypt := goalone.New(s.Secret, goalone.Timestamp)

	if strings.Contains(data, "?") {
		urlToSign = fmt.Sprintf("%s&hash=", data)
	} else {
		urlToSign = fmt.Sprintf("%s?hash=", data)
	}

	// sign and return a token
	tokenBytes := crypt.Sign([]byte(urlToSign))
	token := string(tokenBytes)
	return token
}

// VerifyToken - unsignes the token to verify it and returns true if valid
func (s *Signer) VerifyToken(token string) bool {
	// create a new signer using secret
	crypt := goalone.New(s.Secret, goalone.Timestamp)
	_, err := crypt.Unsign([]byte(token))
	if err != nil {
		fmt.Println(err)
		return false
	}

	return true
}

// IsExpired - returns true when the token is expired
func (s *Signer) IsExpired(token string, minutesUntilExpire int) bool {
	// create a new signer using secret
	crypt := goalone.New(s.Secret, goalone.Timestamp)
	ts := crypt.Parse([]byte(token))

	return time.Since(ts.Timestamp) > time.Duration(minutesUntilExpire)*time.Minute
}
