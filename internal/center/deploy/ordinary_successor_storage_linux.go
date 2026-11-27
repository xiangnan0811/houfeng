//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

const ordinaryS3SecretMount = "/run/houfeng/optional-secrets/s3"

type ordinaryAttachmentStorage struct {
	backend, mountSource         string
	endpoint, bucket, secure     string
	accessKeyFile, secretKeyFile string
}

func ordinaryAttachmentStorageFromRecipe(recipe ordinaryNamespaceRecipe) (ordinaryAttachmentStorage, error) {
	value := func(name string) string {
		if entry := recipe.Environment[name]; entry != nil {
			return *entry
		}
		return ""
	}
	storage := ordinaryAttachmentStorage{backend: value("HOUFENG_ATTACHMENT_BLOB_BACKEND")}
	var target string
	var readOnly bool
	switch storage.backend {
	case "local":
		target = value("HOUFENG_ATTACHMENT_BLOB_ROOT")
		if target != composeNamespaceRoot {
			return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
		}
	case "s3":
		target, readOnly = ordinaryS3SecretMount, true
		storage.endpoint = value("HOUFENG_ATTACHMENT_S3_ENDPOINT")
		storage.bucket = value("HOUFENG_ATTACHMENT_S3_BUCKET")
		storage.secure = value("HOUFENG_ATTACHMENT_S3_SECURE")
		storage.accessKeyFile = value("HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE")
		storage.secretKeyFile = value("HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE")
		if storage.endpoint == "" || storage.bucket == "" || storage.accessKeyFile == "" || storage.secretKeyFile == "" {
			return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
		}
	default:
		return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
	}
	for _, volume := range recipe.Volumes {
		if volume.Target != target {
			continue
		}
		if storage.mountSource != "" || volume.Type != "bind" || volume.ReadOnly != readOnly || !filepath.IsAbs(volume.Source) || filepath.Clean(volume.Source) != volume.Source || volume.Source == "/" {
			return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
		}
		storage.mountSource = volume.Source
	}
	if storage.mountSource == "" {
		return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
	}
	return storage, nil
}

// Read only the selected storage configuration. Never request the full
// environment or read the credential files through Docker.
func ordinaryReadAttachmentStorage(ctx context.Context, containerID string) (ordinaryAttachmentStorage, error) {
	const env = `{{range .Config.Env}}{{$key := index (split . "=") 0}}{{if or (eq $key "HOUFENG_ATTACHMENT_BLOB_BACKEND" "HOUFENG_ATTACHMENT_BLOB_ROOT") (eq $key "HOUFENG_ATTACHMENT_S3_ENDPOINT" "HOUFENG_ATTACHMENT_S3_BUCKET" "HOUFENG_ATTACHMENT_S3_SECURE") (eq $key "HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE" "HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE")}}{{json .}}{{println}}{{end}}{{end}}`
	const mounts = `{{range .Mounts}}{{if eq .Destination "/var/lib/houfeng/attachments" "/run/houfeng/optional-secrets/s3"}}{{json .}}{{println}}{{end}}{{end}}`
	if !fenceID.MatchString(containerID) {
		return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
	}
	body, err := fenceDocker(ctx, "inspect", "--format", env+mounts, containerID)
	if err != nil {
		return ordinaryAttachmentStorage{}, err
	}
	recipe := ordinaryNamespaceRecipe{Environment: map[string]*string{}}
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if line[0] == '"' {
			var setting string
			if json.Unmarshal(line, &setting) != nil {
				return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
			}
			key, value, ok := strings.Cut(setting, "=")
			if !ok || recipe.Environment[key] != nil {
				return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
			}
			recipe.Environment[key] = &value
		} else {
			var mount struct {
				Type, Source, Destination string
				RW                        bool
			}
			if json.Unmarshal(line, &mount) != nil {
				return ordinaryAttachmentStorage{}, errOrdinaryUpgrade
			}
			recipe.Volumes = append(recipe.Volumes, ordinaryRecipeVolume{Type: mount.Type, Source: mount.Source, Target: mount.Destination, ReadOnly: !mount.RW})
		}
	}
	return ordinaryAttachmentStorageFromRecipe(recipe)
}

func ordinaryVerifySuccessorStorage(ctx context.Context, inventory fenceJournal, services ordinaryUpgradeServiceRecipes) error {
	center, err := ordinaryAttachmentStorageFromRecipe(services.Center)
	processor, processorErr := ordinaryAttachmentStorageFromRecipe(services.Processor)
	namespace, namespaceErr := ordinaryNamespaceMountsFromRecipe(services.Namespace)
	if err != nil || processorErr != nil || namespaceErr != nil || center != processor || namespace.backend != center.backend || (center.backend == "local" && namespace.blobRoot != center.mountSource) {
		return errOrdinaryUpgrade
	}
	for _, service := range []string{"houfeng", "houfeng-content-processor"} {
		var id string
		for _, container := range inventory.Containers {
			if container.Service == service {
				if id != "" {
					return errOrdinaryUpgrade
				}
				id = container.ID
			}
		}
		observed, err := ordinaryReadAttachmentStorage(ctx, id)
		if err != nil || observed != center {
			return errOrdinaryUpgrade
		}
	}
	return nil
}
