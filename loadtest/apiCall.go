package loadtest

import (
	"io"
	"net/http"
)

func HitGetEndpoint(targetURL string) (int, error) {

	resp, err := http.Get(targetURL)

	if err != nil {
		return resp.StatusCode, err
	}

	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)

	if err != nil {
		return resp.StatusCode, err
	}

	return resp.StatusCode, nil
}
