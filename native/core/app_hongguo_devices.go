package core

import (
	"context"
	"errors"
	"time"
)

func (engine *nativeEngine) hongguoDevices(ctx context.Context, register bool) (map[string]any, error) {
	if !nativeSourceAvailable(sourceHongguo) {
		return map[string]any{"ready": true}, nil
	}
	downloader := engine.downloader
	client := downloader.hongguoClient()
	persist := func() error {
		engine.mu.Lock()
		engine.hongguoCatalog = downloader.hongguoCatalogSnapshot()
		err := engine.writeCatalogDiskLocked()
		engine.mu.Unlock()
		return err
	}
	if register {
		client.deviceMu.Lock()
		client.registerPausedUntil = time.Time{}
		client.registerPauseErr = nil
		client.deviceMu.Unlock()
		for _, helper := range []bool{false, true} {
			if err := downloader.ensureHongguoDeviceFor(ctx, helper, ""); err != nil {
				if saveErr := persist(); saveErr != nil {
					return nil, errors.Join(err, saveErr)
				}
				return nil, err
			}
			if err := persist(); err != nil {
				return nil, err
			}
		}
	}
	primaryID, primaryInstall, primary := client.device(false)
	helperID, helperInstall, helper := client.device(true)
	primaryReady := primary && hongguoNumericID.MatchString(primaryID) && hongguoNumericID.MatchString(primaryInstall) && primaryID != "0" && primaryInstall != "0"
	helperReady := helper && hongguoNumericID.MatchString(helperID) && hongguoNumericID.MatchString(helperInstall) && helperID != "0" && helperInstall != "0"
	return map[string]any{"ready": primaryReady && helperReady}, nil
}
