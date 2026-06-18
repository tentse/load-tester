package loadtest

import (
	"io"
	"net/http"
	"strings"
)

func HitGetEndpoint(targetURL, token string) (int, error) {

	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, err
	}

	if token != "" {
		req.Header.Add("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}

func HitPostEndpoint(targetURL, token, reqBody string) (int, error) {

	reqBodyReader := strings.NewReader(reqBody)

	req, err := http.NewRequest(http.MethodPost, targetURL, reqBodyReader)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Add("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil

}
