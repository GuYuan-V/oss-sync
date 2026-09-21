package serverplugin

import (
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/pkg/ossplugin"
)

// modelWireResult 将结果转换为 SDK 协议类型，不直接使用 GORM 模型的 JSON 形态
func modelWireResult(value any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	switch row := value.(type) {
	case models.User:
		return ossplugin.User{ID: row.ID, Username: row.Username, Role: row.Role}, nil
	case []models.User:
		result := make([]ossplugin.User, 0, len(row))
		for _, item := range row {
			result = append(result, ossplugin.User{ID: item.ID, Username: item.Username, Role: item.Role})
		}
		return result, nil
	case models.Vault:
		return ossplugin.Vault{ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, Description: row.Description, StorageQuota: row.StorageQuota, StorageUsed: row.StorageUsed}, nil
	case []models.Vault:
		result := make([]ossplugin.Vault, 0, len(row))
		for _, item := range row {
			result = append(result, ossplugin.Vault{ID: item.ID, OwnerID: item.OwnerID, Name: item.Name, Description: item.Description, StorageQuota: item.StorageQuota, StorageUsed: item.StorageUsed})
		}
		return result, nil
	case models.File:
		return ossplugin.File{ID: row.ID, UserID: row.UserID, VaultID: row.VaultID, Path: row.Path, Type: row.Type, Hash: row.Hash, Size: row.Size, Revision: row.Revision, IsDeleted: row.IsDeleted}, nil
	case []models.File:
		result := make([]ossplugin.File, 0, len(row))
		for _, item := range row {
			result = append(result, ossplugin.File{ID: item.ID, UserID: item.UserID, VaultID: item.VaultID, Path: item.Path, Type: item.Type, Hash: item.Hash, Size: item.Size, Revision: item.Revision, IsDeleted: item.IsDeleted})
		}
		return result, nil
	case models.Share:
		return ossplugin.Share{ShareID: row.ShareID, UserID: row.UserID, VaultID: row.VaultID, TargetPath: row.TargetPath, IsFolder: row.IsFolder, AllowCopy: row.AllowCopy, Views: row.Views}, nil
	case []models.Share:
		result := make([]ossplugin.Share, 0, len(row))
		for _, item := range row {
			result = append(result, ossplugin.Share{ShareID: item.ShareID, UserID: item.UserID, VaultID: item.VaultID, TargetPath: item.TargetPath, IsFolder: item.IsFolder, AllowCopy: item.AllowCopy, Views: item.Views})
		}
		return result, nil
	case models.ClientDevice:
		return ossplugin.Device{ID: row.ID, UserID: row.UserID, ClientID: row.ClientID, Name: row.Name, Status: row.Status}, nil
	case []models.ClientDevice:
		result := make([]ossplugin.Device, 0, len(row))
		for _, item := range row {
			result = append(result, ossplugin.Device{ID: item.ID, UserID: item.UserID, ClientID: item.ClientID, Name: item.Name, Status: item.Status})
		}
		return result, nil
	case models.Collaboration:
		return ossplugin.Collaboration{ID: row.ID, VaultID: row.VaultID, FileID: row.FileID, OwnerID: row.OwnerID, CollaboratorID: row.CollaboratorID, Status: row.Status}, nil
	case []models.Collaboration:
		result := make([]ossplugin.Collaboration, 0, len(row))
		for _, item := range row {
			result = append(result, ossplugin.Collaboration{ID: item.ID, VaultID: item.VaultID, FileID: item.FileID, OwnerID: item.OwnerID, CollaboratorID: item.CollaboratorID, Status: item.Status})
		}
		return result, nil
	default:
		return value, nil
	}
}
