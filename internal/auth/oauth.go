package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type OAuthProvider interface {
	GetAuthURL(state string) string
	ExchangeCode(code string) (*OAuthUserInfo, error)
}

type OAuthUserInfo struct {
	ProviderID string
	Email      string
	FullName   string
	AvatarURL  string
	Login      string
}

// VKProvider реализует OAuth 2.0 через ВКонтакте
type VKProvider struct {
	Config OAuthConfig
}

func (p *VKProvider) GetAuthURL(state string) string {
	return fmt.Sprintf(
		"https://oauth.vk.com/authorize?client_id=%s&display=page&redirect_uri=%s&scope=email&response_type=code&v=5.131&state=%s",
		p.Config.ClientID, url.QueryEscape(p.Config.RedirectURL), state,
	)
}

func (p *VKProvider) ExchangeCode(code string) (*OAuthUserInfo, error) {
	// VK возвращает email прямо в теле ответа на обмен кода (нестандартно)
	tokenURL := fmt.Sprintf(
		"https://oauth.vk.com/access_token?client_id=%s&client_secret=%s&redirect_uri=%s&code=%s",
		p.Config.ClientID, p.Config.ClientSecret, url.QueryEscape(p.Config.RedirectURL), code,
	)
	resp, err := http.Get(tokenURL)
	if err != nil {
		return nil, fmt.Errorf("VK token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		UserID      int64  `json:"user_id"`
		Email       string `json:"email"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("VK token parse: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("VK: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	// Получаем имя и аватар
	userURL := fmt.Sprintf(
		"https://api.vk.com/method/users.get?access_token=%s&fields=first_name,last_name,photo_100&v=5.131",
		tokenResp.AccessToken,
	)
	userResp, err := http.Get(userURL)
	if err != nil {
		return nil, fmt.Errorf("VK user info request: %w", err)
	}
	defer userResp.Body.Close()
	userBody, _ := io.ReadAll(userResp.Body)

	var userAPI struct {
		Response []struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Photo100  string `json:"photo_100"`
		} `json:"response"`
	}
	if err := json.Unmarshal(userBody, &userAPI); err != nil || len(userAPI.Response) == 0 {
		return nil, fmt.Errorf("VK user info parse failed")
	}

	u := userAPI.Response[0]
	fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)

	return &OAuthUserInfo{
		ProviderID: fmt.Sprintf("%d", u.ID),
		Email:      tokenResp.Email,
		FullName:   fullName,
		AvatarURL:  u.Photo100,
		Login:      fmt.Sprintf("vk_%d", u.ID),
	}, nil
}

// YandexProvider реализует OAuth 2.0 через Яндекс ID
type YandexProvider struct {
	Config OAuthConfig
}

func (p *YandexProvider) GetAuthURL(state string) string {
	return fmt.Sprintf(
		"https://oauth.yandex.ru/authorize?response_type=code&client_id=%s&state=%s",
		p.Config.ClientID, state,
	)
}

func (p *YandexProvider) ExchangeCode(code string) (*OAuthUserInfo, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", p.Config.ClientID)
	data.Set("client_secret", p.Config.ClientSecret)

	resp, err := http.PostForm("https://oauth.yandex.ru/token", data)
	if err != nil {
		return nil, fmt.Errorf("Yandex token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("Yandex token parse: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("Yandex: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	req, _ := http.NewRequest("GET", "https://login.yandex.ru/info?format=json", nil)
	req.Header.Set("Authorization", "OAuth "+tokenResp.AccessToken)

	userResp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("Yandex user info request: %w", err)
	}
	defer userResp.Body.Close()
	userBody, _ := io.ReadAll(userResp.Body)

	var userInfo struct {
		ID              string `json:"id"`
		Login           string `json:"login"`
		RealName        string `json:"real_name"`
		FirstName       string `json:"first_name"`
		LastName        string `json:"last_name"`
		DefaultEmail    string `json:"default_email"`
		DefaultAvatarID string `json:"default_avatar_id"`
	}
	if err := json.Unmarshal(userBody, &userInfo); err != nil {
		return nil, fmt.Errorf("Yandex user info parse: %w", err)
	}

	fullName := userInfo.RealName
	if fullName == "" {
		fullName = strings.TrimSpace(userInfo.FirstName + " " + userInfo.LastName)
	}
	if fullName == "" {
		fullName = userInfo.Login
	}

	avatarURL := ""
	if userInfo.DefaultAvatarID != "" {
		avatarURL = fmt.Sprintf("https://avatars.yandex.net/get-yapic/%s/islands-200", userInfo.DefaultAvatarID)
	}

	return &OAuthUserInfo{
		ProviderID: userInfo.ID,
		Email:      userInfo.DefaultEmail,
		FullName:   fullName,
		AvatarURL:  avatarURL,
		Login:      "ya_" + userInfo.Login,
	}, nil
}
