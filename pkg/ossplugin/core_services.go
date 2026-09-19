package ossplugin

import "context"

type VaultInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type FileQuery struct {
	VaultID string `json:"vault_id"`
	Path    string `json:"path,omitempty"`
}

type ShareInput struct {
	VaultID    string `json:"vault_id"`
	TargetPath string `json:"target_path"`
	IsFolder   bool   `json:"is_folder,omitempty"`
	AllowCopy  bool   `json:"allow_copy,omitempty"`
}

type BlogContent struct {
	VaultID string `json:"vault_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s VaultService) Get(ctx context.Context, vaultID string) (Vault, error) {
	var result Vault
	err := s.client.HostCall(ctx, "host.vault.get", map[string]any{"vault_id": vaultID}, &result)
	return result, err
}

func (s VaultService) Create(ctx context.Context, input VaultInput) (Vault, error) {
	var result Vault
	err := s.client.HostCall(ctx, "host.vault.create", input, &result)
	return result, err
}

func (s VaultService) Update(ctx context.Context, vaultID string, input VaultInput) (Vault, error) {
	var result Vault
	err := s.client.HostCall(ctx, "host.vault.update", map[string]any{"vault_id": vaultID, "input": input}, &result)
	return result, err
}

func (s VaultService) Delete(ctx context.Context, vaultID string) error {
	var result ExecResult
	return s.client.HostCall(ctx, "host.vault.delete", map[string]any{"vault_id": vaultID}, &result)
}

func (s FileService) Get(ctx context.Context, query FileQuery) (File, error) {
	var result File
	err := s.client.HostCall(ctx, "host.file.get", query, &result)
	return result, err
}

func (s FileService) ListVault(ctx context.Context, vaultID string, limit int) ([]File, error) {
	return modelListWithParams[File](ctx, s.client, "files", limit, map[string]any{"vault_id": vaultID})
}

func (s ShareService) Create(ctx context.Context, input ShareInput) (Share, error) {
	var result Share
	err := s.client.HostCall(ctx, "host.share.create", input, &result)
	return result, err
}

func (s ShareService) Update(ctx context.Context, shareID string, allowCopy bool) (Share, error) {
	var result Share
	err := s.client.HostCall(ctx, "host.share.update", map[string]any{"share_id": shareID, "allow_copy": allowCopy}, &result)
	return result, err
}

func (s ShareService) Delete(ctx context.Context, shareID string) error {
	var result ExecResult
	return s.client.HostCall(ctx, "host.share.delete", map[string]any{"share_id": shareID}, &result)
}

func (s ServiceClient) Blog() BlogService { return BlogService{client: s.client} }

type BlogService struct{ client *Client }

func (s BlogService) GetMarkdown(ctx context.Context, vaultID, path string) (BlogContent, error) {
	var result BlogContent
	err := s.client.HostCall(ctx, "host.blog.get", map[string]any{"vault_id": vaultID, "path": path}, &result)
	return result, err
}

func (s BlogService) Filter(ctx context.Context, hook string, content BlogContent) (BlogContent, error) {
	var result BlogContent
	err := s.client.HostCall(ctx, "host.blog.filter", map[string]any{"hook": hook, "content": content}, &result)
	return result, err
}

func modelListWithParams[T any](ctx context.Context, client *Client, model string, limit int, where map[string]any) ([]T, error) {
	var result []T
	err := client.HostCall(ctx, "host.model.list", map[string]any{"model": model, "limit": limit, "where": where}, &result)
	return result, err
}
