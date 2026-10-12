package projects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type IdentityManager struct {
	BaseURL, Realm, AdminUsername, AdminPassword string
	Client                                       *http.Client
}
type Identity struct{ ID, Username, Email string }

func (m *IdentityManager) token(ctx context.Context) (string, error) {
	body := url.Values{"client_id": {"admin-cli"}, "grant_type": {"password"}, "username": {m.AdminUsername}, "password": {m.AdminPassword}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/realms/master/protocol/openid-connect/token", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("identity administration unavailable")
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out.AccessToken, err
}
func (m *IdentityManager) Create(ctx context.Context, username, email, password string) (Identity, error) {
	var out Identity
	token, err := m.token(ctx)
	if err != nil {
		return out, err
	}
	payload, _ := json.Marshal(map[string]any{"username": username, "email": email, "enabled": true, "emailVerified": true, "credentials": []map[string]any{{"type": "password", "value": password, "temporary": false}}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/admin/realms/"+url.PathEscape(m.Realm)+"/users", strings.NewReader(string(payload)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.Client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return out, errors.New("identity user creation failed")
	}
	parts := strings.Split(resp.Header.Get("Location"), "/")
	if len(parts) == 0 {
		return out, errors.New("identity id missing")
	}
	out = Identity{ID: parts[len(parts)-1], Username: username, Email: email}
	return out, nil
}
func (m *IdentityManager) Delete(ctx context.Context, id string) error {
	token, err := m.token(ctx)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, m.BaseURL+"/admin/realms/"+url.PathEscape(m.Realm)+"/users/"+url.PathEscape(id), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 404 {
		return errors.New("identity user deletion failed")
	}
	return nil
}
