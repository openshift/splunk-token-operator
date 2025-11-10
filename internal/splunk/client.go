// Package splunkapi defines a simple api for creating and deleting Splunk
// HTTP Event Collector (HEC) tokens through Splunk's Admin Config Services
// interface.
package splunkapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/openshift/splunk-token-operator/api/v1alpha1"
)

const (
	acsHostname         string = "https://admin.splunk.com"
	tokenManagementPath string = "adminconfig/v2/inputs/http-event-collectors" // #nosec G101 -- not a credential

	missingSplunkError string = "missing Splunk instance name"
	missingJWTError    string = "missing Splunk authentication token"
)

// A Client contains the information necessary to connect to Splunk ACS for the
// specified instance using a JWT for authentication. The zero value of Client
// does not make any assumptions and contains no information, and the NewClient
// function should be used to create a working connection.
type Client struct {
	jwt    string
	url    string
	client http.Client
}

// The TokenManager interface defines the necessary functions for interacting with Splunk HEC tokens.
// For our purposes the manager only needs to create and delete tokens.
type TokenManager interface {
	CreateToken(context.Context, HECToken) (*HECToken, error)
	DeleteToken(context.Context, string) error
}

// The HECToken struct defines the fields we need for HEC token management.
// The fields we are concerned with for a HEC token are its name,
// its value (the auth token itself),
// and the Splunk indexes the token is able to write to.
type HECToken struct {
	Spec  v1alpha1.SplunkTokenSpec
	Value string `json:"token,omitempty"`
}

type tokenResponse struct {
	Data HECToken `json:"http-event-collector"`
}

type errorResponse struct {
	Code    string
	Message string
}

// NewClient creates a new Splunk Client using the provided instance name and JWT.
func NewClient(splunkStack, jwt string) (*Client, error) {
	if splunkStack == "" {
		return nil, errors.New(missingSplunkError)
	}
	if jwt == "" {
		return nil, errors.New(missingJWTError)
	}

	fullUrl, err := url.JoinPath(acsHostname, splunkStack, tokenManagementPath)
	if err != nil {
		return nil, err
	}
	return &Client{
		jwt:    jwt,
		url:    fullUrl,
		client: http.Client{},
	}, nil
}

// CreateToken takes a HECToken spec and creates a token on the Splunk instance.
// The return value for successful token creation is the HECToken with the secret added to the Value field.
func (c *Client) CreateToken(ctx context.Context, token HECToken) (*HECToken, error) {
	if token.Spec.DefaultIndex != "" && !slices.Contains(token.Spec.AllowedIndexes, token.Spec.DefaultIndex) {
		token.Spec.AllowedIndexes = append(token.Spec.AllowedIndexes, token.Spec.DefaultIndex)
	}
	payload, err := json.Marshal(token.Spec)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.jwt))
	req.Header.Add("Content-Type", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)
	// skip error handling on 409 and retrieve existing token
	if res.StatusCode >= 400 && res.StatusCode != http.StatusConflict {
		response := &errorResponse{}
		if err := decoder.Decode(response); err != nil {
			return nil, err
		}
		return nil, response
	}

	return c.getToken(ctx, token.Spec.Name)
}

// DeleteToken deletes the named token, returning any error from the Splunk server.
func (c *Client) DeleteToken(ctx context.Context, name string) error {
	tokenUri, err := url.JoinPath(c.url, name)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, tokenUri, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.jwt))
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		// HEC token doesn't exist so we're done here
		return nil
	} else if res.StatusCode != http.StatusAccepted {
		decoder := json.NewDecoder(res.Body)
		response := &errorResponse{}
		if err := decoder.Decode(response); err != nil {
			return err
		}
		return response
	}
	return nil
}

func (c *Client) getToken(ctx context.Context, name string) (*HECToken, error) {
	getURL, err := url.JoinPath(c.url, name)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.jwt))
	request.Header.Add("Content-Type", "application/json")

	res, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)

	if res.StatusCode >= 400 {
		response := &errorResponse{}
		if err := decoder.Decode(response); err != nil {
			return nil, err
		}
		return nil, response
	}
	token := &tokenResponse{}
	if err := decoder.Decode(token); err != nil {
		return nil, err
	}
	return &token.Data, nil
}

func (e *errorResponse) Error() string {
	return fmt.Sprintf("received error response %s: %s", e.Code, e.Message)
}
