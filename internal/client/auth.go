// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import "fmt"

// LoginRequest is the body sent to POST /api/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	AuthType string `json:"authType"`
}

// LoginResponse is the data field returned by POST /api/auth/login.
type LoginResponse struct {
	Token string `json:"token"`
}

// Login calls POST /api/auth/login and returns the Bearer token.
// The token is stored in c.Token so subsequent calls on this Client are
// automatically authenticated. authType must be "sylve" or "pam".
func (c *Client) Login(username, password, authType string) (string, error) {
	req := LoginRequest{Username: username, Password: password, AuthType: authType}
	var resp APIResponse[LoginResponse]
	if err := c.post("/auth/login", req, &resp); err != nil {
		return "", fmt.Errorf("sylve login as %q: %w", username, err)
	}
	c.Token = resp.Data.Token
	return c.Token, nil
}

// Logout calls POST /api/auth/logout to invalidate the current token.
func (c *Client) Logout() error {
	// Sylve's POST /auth/logout returns an HTML page (the SvelteKit SPA),
	// not JSON. Pass nil as the decode target so the body is discarded.
	if err := c.post("/auth/logout", nil, nil); err != nil {
		return fmt.Errorf("sylve logout: %w", err)
	}
	c.Token = ""
	return nil
}
