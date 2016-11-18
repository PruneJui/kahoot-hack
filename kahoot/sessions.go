package kahoot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

var bruteForceErr = errors.New("not exactly one possible mask")

const tokenAttempts = 40

func gameSessionToken(gamePin int) (string, error) {
	for i := 0; i < tokenAttempts; i++ {
		token, err := attemptGameSessionToken(gamePin, false)
		if err != bruteForceErr {
			return token, err
		}
	}
	token, err := attemptGameSessionToken(gamePin, true)
	if err == nil {
		return token, nil
	}
	return "", errors.New("could not defeat session challenge")
}

func attemptGameSessionToken(gamePin int, useEval bool) (string, error) {
	resp, err := http.Get("https://kahoot.it/reserve/session/" + strconv.Itoa(gamePin))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	token := resp.Header.Get("X-Kahoot-Session-Token")
	var bodyObj struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &bodyObj); err != nil {
		if string(body) == "Not found" {
			return "", fmt.Errorf("game pin not found: %d", gamePin)
		}
		return "", fmt.Errorf("parse session challenge: %s", err)
	}

	return decipherToken(token, bodyObj.Challenge, useEval)
}

func decipherToken(xToken, challenge string, useEval bool) (string, error) {
	r := bytes.NewReader([]byte(xToken))
	base64Dec := base64.NewDecoder(base64.StdEncoding, r)
	rawToken, err := ioutil.ReadAll(base64Dec)
	if err != nil {
		return "", fmt.Errorf("parse session token: %s", err)
	}

	mask, err := computeChallenge(challenge, useEval)
	if err != nil {
		mask, err = bruteForceChallenge(rawToken)
		if err != nil {
			return "", err
		}
	}

	for i := range rawToken {
		rawToken[i] ^= mask[i%len(mask)]
	}

	return string(rawToken), nil
}

func computeChallenge(ch string, useEval bool) ([]byte, error) {
	if useEval {
		evalURL := url.URL{
			Scheme:   "http",
			Host:     "safeval.pw",
			Path:     "/eval",
			RawQuery: url.Values{"code": []string{ch}}.Encode(),
		}
		resp, err := http.Get(evalURL.String())
		if resp != nil {
			defer resp.Body.Close()
		}
		if err != nil {
			return nil, err
		}
		return ioutil.ReadAll(resp.Body)
	}

	challengeExpr := regexp.MustCompile("^\\(([0-9]*)\\s*\\+\\s*([0-9]*)\\)\\s*\\*\\s*([0-9]*)$")
	match := challengeExpr.FindStringSubmatch(ch)
	if match != nil {
		num1, _ := strconv.Atoi(match[1])
		num2, _ := strconv.Atoi(match[2])
		num3, _ := strconv.Atoi(match[3])
		return []byte(strconv.Itoa((num1 + num2) * num3)), nil
	}
	challengeExpr = regexp.MustCompile("^([0-9]*)\\s*\\*\\s*\\(([0-9]*)\\s*\\+\\s*([0-9]*)\\)$")
	match = challengeExpr.FindStringSubmatch(ch)
	if match != nil {
		num1, _ := strconv.Atoi(match[1])
		num2, _ := strconv.Atoi(match[2])
		num3, _ := strconv.Atoi(match[3])
		return []byte(strconv.Itoa(num1 * (num2 + num3))), nil
	}
	return nil, fmt.Errorf("unsupported challenge: %s", ch)
}

func bruteForceChallenge(rawToken []byte) ([]byte, error) {
	var possibilities [][]byte
LengthLoop:
	for n := 1; n < 9; n++ {
		possible := make([]byte, n)
		for i := range possible {
			possible[i] = possibleMaskByte(rawToken, n, i)
			if possible[i] == 0 {
				continue LengthLoop
			}
		}
		possibilities = append(possibilities, possible)
	}
	for i := 1; i < len(possibilities); i++ {
		if masksEquivalent(possibilities[0], possibilities[i]) {
			possibilities[i] = possibilities[len(possibilities)-1]
			possibilities = possibilities[:len(possibilities)-1]
			i--
		}
	}
	if len(possibilities) != 1 {
		return nil, bruteForceErr
	}
	return possibilities[0], nil
}

func possibleMaskByte(rawToken []byte, chLen, byteIdx int) byte {
	possibs := []byte{}
PossibilityLoop:
	for _, r := range "-0123456789." {
		numChar := byte(r)
		for i := byteIdx; i < len(rawToken); i += chLen {
			masked := rawToken[i] ^ numChar
			if !((masked >= 'a' && masked <= 'f') || (masked >= '0' && masked <= '9')) {
				continue PossibilityLoop
			}
		}
		possibs = append(possibs, numChar)
	}
	if len(possibs) != 1 {
		return 0
	}
	return possibs[0]
}

func masksEquivalent(m1, m2 []byte) bool {
	rep1 := append([]byte{}, m1...)
	rep2 := append([]byte{}, m2...)
	for len(rep1) != len(rep2) {
		if len(rep1) < len(rep2) {
			rep1 = append(rep1, m1...)
		} else {
			rep2 = append(rep2, m2...)
		}
	}
	return bytes.Equal(rep1, rep2)
}