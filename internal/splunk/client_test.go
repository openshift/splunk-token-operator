//nolint:errcheck,goconst
package splunkapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCreateClient(t *testing.T) {
	t.Run("successfully creates", func(t *testing.T) {
		want := Client{
			url: "https://admin.splunk.com/mock_splunk/adminconfig/v2/inputs/http-event-collectors",
			jwt: "foo",
		}

		got, err := NewClient("mock_splunk", "foo")
		if err != nil {
			t.Fatalf("got unexpected error: %s", err)
		}
		if got.url != want.url {
			t.Errorf("expected url %s but got %s", want.url, got.url)
		}
		if got.jwt != want.jwt {
			t.Errorf("expected jwt %s but got %s", want.jwt, got.jwt)
		}
	})

	t.Run("returns error if no stack is provided", func(t *testing.T) {
		_, err := NewClient("", "foo")
		if err == nil {
			t.Fatal("expected error but did not get one")
		}
		if err.Error() != missingSplunkError {
			t.Errorf("expected error for missing Splunk name but got %v", err)
		}
	})

	t.Run("returns error if no auth token is provided", func(t *testing.T) {
		_, err := NewClient("mock_splunk", "")
		if err == nil {
			t.Fatal("expected error but did not get one")
		}
		if err.Error() != missingJWTError {
			t.Errorf("expected error for missing auth token but got %v", err)
		}
	})
}

func TestCreateToken(t *testing.T) {
	t.Run("request is formatted properly", func(t *testing.T) {
		postPath := "/mock_splunk/adminconfig/v2/inputs/http-event-collectors"
		getPath := "/mock_splunk/adminconfig/v2/inputs/http-event-collectors/bar"
		wantAuth := "Bearer foo"
		wantContent := "application/json"
		wantBody := `{"name":"bar"}`
		var serverCalls uint

		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serverCalls += 1
			switch r.Method {
			case http.MethodPost:
				if r.URL.Path != postPath {
					t.Errorf("expected POST request to %s but got %s", postPath, r.URL.Path)
				}

				authHeader := r.Header.Get("Authorization")
				if authHeader != wantAuth {
					t.Errorf("expected header Authorization with value '%s' but got '%s'", wantAuth, authHeader)
				}

				contentType := r.Header.Get("Content-Type")
				if contentType != wantContent {
					t.Errorf("expected header Content-Type with value '%s' but got '%s'", wantContent, contentType)
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("got unexpected error: %s", err)
				}
				if string(body) != wantBody {
					t.Errorf("expected request payload '%s' but got '%s'", wantBody, body)
				}
			case http.MethodGet:
				if r.URL.Path != getPath {
					t.Errorf("expected GET request to %s but got %s", getPath, r.URL.Path)
				}
				authHeader := r.Header.Get("Authorization")
				if authHeader != wantAuth {
					t.Errorf("expected header Authorization with value '%s' but got '%s'", wantAuth, authHeader)
				}
			}
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)

		testClient.CreateToken(t.Context(), &HECToken{Name: "bar"})
		if serverCalls == 0 {
			t.Errorf("no request made to test server")
		}
	})

	t.Run("handles OK response", func(t *testing.T) {
		tokenName := "bar"
		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			createResponse := `{"http-event-collector":{"spec":{"name":"bar"}}}`
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, createResponse)
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)

		token := &HECToken{Name: tokenName}
		token, err := testClient.CreateToken(t.Context(), token)
		if err != nil {
			t.Errorf("error creating token: %s", err)
		}

		// creation response from Splunk is just the token's name
		if token.Name != tokenName {
			t.Errorf("expected Name='%s' but got '%s'", tokenName, token.Name)
		}
		if token.Value != "" {
			t.Errorf("expected empty Value but got %s", token.Value)
		}
		if token.DefaultIndex != "" {
			t.Errorf("expected empty DefaultIndex but got %s", token.DefaultIndex)
		}
		if token.AllowedIndexes != nil {
			t.Errorf("expected empty AllowedIndexes but got %v", token.AllowedIndexes)
		}
	})

	t.Run("creates with default and allowed indexes", func(t *testing.T) {
		wantName := "bar"
		wantIndexes := []string{"audit_index", "other_index"}
		wantDefault := "audit_index"
		wantValue := "UUID-VALUE"
		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var createResponse string
			switch r.Method {
			case http.MethodPost:
				createResponse = `{"http-event-collector":{"spec":{"name":"bar"}}}`
				w.WriteHeader(http.StatusAccepted)
			case http.MethodGet:
				createResponse = `{"http-event-collector":{"spec":{"name":"bar","defaultIndex":"audit_index","allowedIndexes":["audit_index","other_index"]},"token":"UUID-VALUE"}}`
			}
			io.WriteString(w, createResponse)
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)

		token := &HECToken{
			Name:           "bar",
			AllowedIndexes: []string{"audit_index", "other_index"},
			DefaultIndex:   "audit_index",
		}
		token, err := testClient.CreateToken(t.Context(), token)
		if err != nil {
			t.Errorf("error creating token: %s", err)
		}

		if token.Name != wantName {
			t.Errorf("expected Name '%s' but got '%s'", wantName, token.Name)
		}
		if token.Value != wantValue {
			t.Errorf("expected Value %s but got %s", wantValue, token.Value)
		}
		if token.DefaultIndex != wantDefault {
			t.Errorf("expected DefaultIndex %s but got %s", wantDefault, token.DefaultIndex)
		}
		if !reflect.DeepEqual(wantIndexes, token.AllowedIndexes) {
			t.Errorf("expected AllowedIndexes %v but got %v", wantIndexes, token.AllowedIndexes)
		}
	})
	t.Run("handles errors", func(t *testing.T) {
		wantError := "received error response 400-oh-no-it-broke: halt and catch fire"
		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			errorJSON := `{"code":"400-oh-no-it-broke","message":"halt and catch fire"}`
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, errorJSON)
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)

		_, err := testClient.CreateToken(t.Context(), &HECToken{Name: "bar"})
		if err == nil {
			t.Fatal("expected error but did not receive one")
		}
		if err.Error() != wantError {
			t.Errorf("did not receive expected error message, got %s", err)
		}
	})
}

func TestDeleteToken(t *testing.T) {
	t.Run("request is formatted properly", func(t *testing.T) {
		tokenName := "bar"
		wantMethod := http.MethodDelete
		wantPath := "/mock_splunk/adminconfig/v2/inputs/http-event-collectors/bar"
		wantAuth := "Bearer foo"
		var serverCalls uint

		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serverCalls += 1
			if r.Method == http.MethodDelete {
				if r.Method != wantMethod {
					t.Errorf("expected %s request but got %s", wantMethod, r.Method)
				}

				if r.URL.Path != wantPath {
					t.Errorf("expected request to %s but got %s", wantPath, r.URL.Path)
				}

				authHeader := r.Header.Get("Authorization")
				if authHeader != wantAuth {
					t.Errorf("expected header Authorization with value '%s' but got '%s'", wantAuth, authHeader)
				}
			}
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)
		testClient.DeleteToken(t.Context(), tokenName)
		if serverCalls == 0 {
			t.Errorf("no request made to test server")
		}
	})

	t.Run("handles successful deletion", func(t *testing.T) {
		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, "")
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)
		err := testClient.DeleteToken(t.Context(), "bar")
		if err != nil {
			t.Errorf("got unexpected error %s", err)
		}
	})

	t.Run("handles deletion errors", func(t *testing.T) {
		wantError := "received error response 400-oh-no-it-broke: halt and catch fire"
		splunkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			errorJSON := `{"code":"400-oh-no-it-broke","message":"halt and catch fire"}`
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, errorJSON)
		}))
		defer splunkServer.Close()

		testClient := createTestClient(splunkServer.URL)

		err := testClient.DeleteToken(t.Context(), "bar")
		if err == nil {
			t.Fatal("expected error but did not receive one")
		}
		if err.Error() != wantError {
			t.Errorf("did not receive expected error message, got %s", err)
		}
	})
}

// helper function to create a Client with the hostname set to the URL of the test server
func createTestClient(testHostname string) *Client {
	c, _ := NewClient("mock_splunk", "foo")
	c.url = strings.Replace(c.url, acsHostname, testHostname, 1)
	return c
}
