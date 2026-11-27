package homelabk3s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	"github.com/google/uuid"
)

const (
	contaboAuthURL = "https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token"
	contaboAPIURL  = "https://api.contabo.com/v1"
)

type contaboAPI struct {
	client *http.Client
}

func (api contaboAPI) request(ctx context.Context, method, endpoint, token string, body io.Reader, failure string, out any) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if endpoint == contaboAuthURL {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", uuid.NewString())
		req.Header.Set("X-Trace-ID", uuid.NewString())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	client := api.client
	if client == nil {
		client = http.DefaultClient
	}
	// fetchWithTimeout stops its timer when fetch receives the response headers.
	timeoutError := errors.New("Timed out after 15000ms")
	timer := time.AfterFunc(15*time.Second, func() { cancel(timeoutError) })
	resp, err := client.Do(req)
	timer.Stop()
	if err != nil {
		if errors.Is(context.Cause(ctx), timeoutError) {
			return timeoutError
		}
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, statusText, _ := strings.Cut(resp.Status, " ")
		return fmt.Errorf("%s: %d %s: %s", failure, resp.StatusCode, statusText, data)
	}
	return json.Unmarshal(data, out)
}

func (api contaboAPI) accessToken(ctx context.Context, creds ContaboCredentialsArgs) (string, error) {
	form := url.Values{
		"client_id": {*creds.ClientID}, "client_secret": {*creds.ClientSecret},
		"username": {*creds.User}, "password": {*creds.Pass}, "grant_type": {"password"},
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	err := api.request(ctx, http.MethodPost, contaboAuthURL, "", strings.NewReader(form.Encode()), "Failed to fetch Contabo access token", &response)
	if err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", errors.New("Contabo token response missing access_token")
	}
	return response.AccessToken, nil
}

func contaboInstanceURL(instanceID string) string {
	// encodeURIComponent leaves these characters unescaped and encodes spaces as %20.
	encoded := strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*").Replace(url.QueryEscape(instanceID))
	return contaboAPIURL + "/compute/instances/" + encoded
}

func (api contaboAPI) instance(ctx context.Context, token, instanceID string) (any, error) {
	var response any
	if err := api.request(ctx, http.MethodGet, contaboInstanceURL(instanceID), token, nil, "Failed to fetch Contabo instance "+instanceID, &response); err != nil {
		return nil, err
	}
	data := jsonField(response, "data")
	if items, ok := data.([]any); ok {
		if len(items) == 0 {
			return nil, nil
		}
		return items[0], nil
	}
	if data != nil {
		return data, nil
	}
	return response, nil
}

func jsonField(value any, key string) any {
	object, _ := value.(map[string]any)
	return object[key]
}

func contaboText(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case float64:
		return jscompat.FormatNumber(value)
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			parts[i] = contaboText(item)
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	default:
		return fmt.Sprint(value)
	}
}

func (api contaboAPI) image(ctx context.Context, token, query string) (any, error) {
	name := jscompat.ToLower(jscompat.Trim(query))
	if name == "" {
		return nil, errors.New("findImageByName: query must be non-empty")
	}
	var response any
	if err := api.request(ctx, http.MethodGet, contaboAPIURL+"/compute/images", token, nil, "Failed to list Contabo images", &response); err != nil {
		return nil, err
	}

	var matches []any
	var candidates []string
	images, _ := jsonField(response, "data").([]any)
	for _, image := range images {
		candidate := jscompat.Trim(contaboText(jsonField(image, "name")))
		if jscompat.ToLower(candidate) == name {
			matches = append(matches, image)
		}
		if candidate != "" && len(candidates) < 15 {
			candidates = append(candidates, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		var ids []string
		for _, match := range matches {
			id := jsonField(match, "imageId")
			if id == nil {
				id = jsonField(match, "id")
			}
			if text := jscompat.Trim(contaboText(id)); text != "" {
				ids = append(ids, text)
				if len(ids) == 10 {
					break
				}
			}
		}
		return nil, fmt.Errorf("Ambiguous Contabo image name '%s' (matched %d images). Matching imageIds: %s", query, len(matches), strings.Join(ids, ", "))
	}
	return nil, fmt.Errorf("No Contabo image matched name '%s'. First %d image names: %s", query, len(candidates), strings.Join(candidates, " | "))
}

func contaboFingerprint(instanceID string, data any) map[string]any {
	result := map[string]any{"instanceId": instanceID}
	for _, field := range []string{"name", "displayName", "status", "region", "regionName", "productId",
		"productName", "productType", "ramMb", "osType", "imageId", "defaultUser", "createdDate", "vHostName"} {
		result[field] = jsonField(data, field)
	}
	for _, field := range []string{"instanceId", "cpuCores", "diskMb", "vHostId", "vHostNumber"} {
		key := field
		if field == "instanceId" {
			key = "instanceIdNumeric"
		}
		result[key] = nil
		if value, ok := jsonField(data, field).(float64); ok {
			result[key] = value
		}
	}

	ipConfig := jsonField(data, "ipConfig")
	result["ipv4"] = jsonField(jsonField(ipConfig, "v4"), "ip")
	for _, field := range []string{"ipAddress", "ipv4"} {
		if result["ipv4"] == nil {
			result["ipv4"] = jsonField(data, field)
		}
	}
	result["ipv6"] = jsonField(jsonField(ipConfig, "v6"), "ip")

	result["datacenter"] = jsonField(data, "dataCenter")
	if result["datacenter"] == nil {
		result["datacenter"] = jsonField(data, "datacenter")
	}
	result["sshKeys"] = nil
	if keys, ok := jsonField(data, "sshKeys").([]any); ok {
		result["sshKeys"] = keys
	}
	return result
}

type contaboSecret struct {
	SecretID *float64 `json:"secretId"`
	Name     string   `json:"name"`
}

func (api contaboAPI) upsertSSHKey(ctx context.Context, token, name, key string) (*float64, error) {
	var listed struct {
		Data []contaboSecret `json:"data"`
	}
	query := url.Values{"type": {"ssh"}, "name": {name}}
	endpoint := contaboAPIURL + "/secrets"
	if err := api.request(ctx, http.MethodGet, endpoint+"?"+query.Encode(), token, nil, "Failed to list Contabo secrets", &listed); err != nil {
		return nil, err
	}
	if listed.Data == nil {
		return nil, errors.New("Contabo list secrets response missing data")
	}

	for _, secret := range listed.Data {
		if secret.Name != name {
			continue
		}
		id := "undefined"
		if secret.SecretID != nil {
			id = jscompat.FormatNumber(*secret.SecretID)
		}
		var patched any
		err := api.sendJSON(ctx, http.MethodPatch, endpoint+"/"+id, token,
			map[string]any{"name": name, "value": key}, "Failed to update Contabo secret "+id, &patched)
		if err != nil {
			return nil, err
		}
		if data, ok := jsonField(patched, "data").([]any); ok && len(data) > 0 {
			return contaboSecretID(data[0]), nil
		}
		return secret.SecretID, nil
	}

	var created any
	if err := api.sendJSON(ctx, http.MethodPost, endpoint, token,
		map[string]any{"name": name, "type": "ssh", "value": key}, "Failed to create Contabo secret '"+name+"'", &created); err != nil {
		return nil, err
	}
	data, _ := jsonField(created, "data").([]any)
	if len(data) == 0 {
		return nil, fmt.Errorf("Contabo create secret response missing data for '%s'", name)
	}
	return contaboSecretID(data[0]), nil
}

func contaboSecretID(value any) *float64 {
	if id, ok := jsonField(value, "secretId").(float64); ok {
		return &id
	}
	return nil
}

func (api contaboAPI) reinstall(ctx context.Context, token, instanceID string, image any, secretID *float64, userData *string) error {
	body := map[string]any{"sshKeys": []*float64{secretID}, "defaultUser": "admin"}
	imageObject, _ := image.(map[string]any)
	if id, ok := imageObject["imageId"]; ok {
		body["imageId"] = id
	}
	if userData != nil && *userData != "" {
		body["userData"] = *userData
	}
	var result any
	return api.sendJSON(ctx, http.MethodPut, contaboInstanceURL(instanceID), token, body, "Failed to reinstall Contabo instance "+instanceID, &result)
}

func (api contaboAPI) sendJSON(ctx context.Context, method, endpoint, token string, body map[string]any, failure string, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return api.request(ctx, method, endpoint, token, bytes.NewReader(data), failure, out)
}
