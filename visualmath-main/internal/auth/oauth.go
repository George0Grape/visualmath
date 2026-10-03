package auth

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    //"net/http"
    "os"
    "strings"
    "time"
    
    "golang.org/x/oauth2"
    "golang.org/x/oauth2/google"
    "golang.org/x/oauth2/vk"
)

// OAuthConfig содержит конфигурации для всех OAuth провайдеров
type OAuthConfig struct {
    VK     *oauth2.Config
    Google *oauth2.Config
    JWTKey string
}

// UserInfo содержит информацию о пользователе из OAuth
type OAuthUserInfo struct {
    ID        string `json:"id"`
    Email     string `json:"email"`
    FirstName string `json:"first_name"`
    LastName  string `json:"last_name"`
    AvatarURL string `json:"avatar"`
    Provider  string `json:"provider"` // "vk" или "google"
}

// NewOAuthConfig создает конфигурацию OAuth
func NewOAuthConfig() *OAuthConfig {
    baseURL := os.Getenv("BASE_URL")
    if baseURL == "" {
        baseURL = "http://localhost:8080"
    }
    
    // Конфигурация VK
    vkConfig := &oauth2.Config{
        ClientID:     os.Getenv("VK_CLIENT_ID"),
        ClientSecret: os.Getenv("VK_CLIENT_SECRET"),
        RedirectURL:  baseURL + "/auth/vk/callback",
        Endpoint:     vk.Endpoint,
        Scopes:       []string{"email"},
    }
    
    // Конфигурация Google
    googleConfig := &oauth2.Config{
        ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
        ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
        RedirectURL:  baseURL + "/auth/google/callback",
        Endpoint:     google.Endpoint,
        Scopes: []string{
            "https://www.googleapis.com/auth/userinfo.email",
            "https://www.googleapis.com/auth/userinfo.profile",
        },
    }
    
    return &OAuthConfig{
        VK:     vkConfig,
        Google: googleConfig,
        JWTKey: os.Getenv("JWT_SECRET"),
    }
}

// GetVKAuthURL возвращает URL для аутентификации через VK
func (c *OAuthConfig) GetVKAuthURL(state string) string {
    return c.VK.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// GetGoogleAuthURL возвращает URL для аутентификации через Google
func (c *OAuthConfig) GetGoogleAuthURL(state string) string {
    return c.Google.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeVKCode обменивает код на токен и получает информацию о пользователе
func (c *OAuthConfig) ExchangeVKCode(ctx context.Context, code string) (*OAuthUserInfo, error) {
    // Получаем токен
    token, err := c.VK.Exchange(ctx, code)
    if err != nil {
        return nil, fmt.Errorf("failed to exchange code: %v", err)
    }
    
    // Получаем информацию о пользователе
    client := c.VK.Client(ctx, token)
    resp, err := client.Get("https://api.vk.com/method/users.get?fields=photo_200,email&v=5.131")
    if err != nil {
        return nil, fmt.Errorf("failed to get user info: %v", err)
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response: %v", err)
    }
    
    // Парсим ответ VK
    var vkResponse struct {
        Response []struct {
            ID        int    `json:"id"`
            FirstName string `json:"first_name"`
            LastName  string `json:"last_name"`
            Photo200  string `json:"photo_200"`
        } `json:"response"`
    }
    
    if err := json.Unmarshal(body, &vkResponse); err != nil {
        return nil, fmt.Errorf("failed to parse VK response: %v", err)
    }
    
    if len(vkResponse.Response) == 0 {
        return nil, fmt.Errorf("no user data in VK response")
    }
    
    user := vkResponse.Response[0]
    
    // Получаем email (для VK нужно отдельно)
    email := token.Extra("email").(string)
    
    return &OAuthUserInfo{
        ID:        fmt.Sprintf("vk_%d", user.ID),
        Email:     email,
        FirstName: user.FirstName,
        LastName:  user.LastName,
        AvatarURL: user.Photo200,
        Provider:  "vk",
    }, nil
}

// ExchangeGoogleCode обменивает код на токен и получает информацию о пользователе
func (c *OAuthConfig) ExchangeGoogleCode(ctx context.Context, code string) (*OAuthUserInfo, error) {
    // Получаем токен
    token, err := c.Google.Exchange(ctx, code)
    if err != nil {
        return nil, fmt.Errorf("failed to exchange code: %v", err)
    }
    
    // Получаем информацию о пользователе
    client := c.Google.Client(ctx, token)
    resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
    if err != nil {
        return nil, fmt.Errorf("failed to get user info: %v", err)
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response: %v", err)
    }
    
    // Парсим ответ Google
    var googleUser struct {
        ID        string `json:"id"`
        Email     string `json:"email"`
        Name      string `json:"name"`
        GivenName string `json:"given_name"`
        FamilyName string `json:"family_name"`
        Picture   string `json:"picture"`
    }
    
    if err := json.Unmarshal(body, &googleUser); err != nil {
        return nil, fmt.Errorf("failed to parse Google response: %v", err)
    }
    
    // Разделяем полное имя на имя и фамилию
    firstName := googleUser.GivenName
    lastName := googleUser.FamilyName
    if firstName == "" && lastName == "" && googleUser.Name != "" {
        names := strings.Split(googleUser.Name, " ")
        if len(names) > 0 {
            firstName = names[0]
        }
        if len(names) > 1 {
            lastName = names[1]
        }
    }
    
    return &OAuthUserInfo{
        ID:        fmt.Sprintf("google_%s", googleUser.ID),
        Email:     googleUser.Email,
        FirstName: firstName,
        LastName:  lastName,
        AvatarURL: googleUser.Picture,
        Provider:  "google",
    }, nil
}

// GenerateStateToken генерирует токен состояния для защиты от CSRF
func GenerateStateToken() string {
    return fmt.Sprintf("%d", time.Now().UnixNano())
}