package core

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func hongguoRegistrationTransport(registers *int) sourceFixtureTransport {
	return func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/service/2/device_register/" {
			return sourceFixtureResponse(request, http.StatusOK, `{"code":0,"data":{}}`), nil
		}
		*registers++
		id := strconv.Itoa(1000 + *registers)
		return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"`+id+`","install_id_str":"`+id+`9"}`), nil
	}
}

func hongguoDevicesReady(t *testing.T, engine *nativeEngine, register bool) bool {
	t.Helper()
	result, err := engine.hongguoDevices(context.Background(), register)
	if err != nil {
		t.Fatal(err)
	}
	return result["ready"] == true
}

func TestHongguoDevicesRegisterBothAndPersist(t *testing.T) {
	registers := 0
	transport := hongguoRegistrationTransport(&registers)
	engine := reopenCatalogEngine(t, t.TempDir(), transport)
	if hongguoDevicesReady(t, engine, false) || registers != 0 {
		t.Fatal("a fresh install reported registered devices")
	}
	if !hongguoDevicesReady(t, engine, true) || registers != 2 {
		t.Fatal("both devices were not registered", registers)
	}
	state := engine.downloader.hongguoCatalogSnapshot()
	if state.DeviceID != "1001" || state.HelperDeviceID != "1002" || !state.Registered || !state.HelperRegistered {
		t.Fatal("registered devices were not kept", state)
	}
	restarted := reopenCatalogEngine(t, engine.directory, transport)
	if !hongguoDevicesReady(t, restarted, false) || registers != 2 {
		t.Fatal("registered devices were not restored after restart", registers)
	}
	if restored := restarted.downloader.hongguoCatalogSnapshot(); restored.DeviceID != "1001" || restored.HelperDeviceID != "1002" {
		t.Fatal("restart changed the registered devices", restored)
	}
}

func TestHongguoDevicesUpgradeRegistersOnlyHelper(t *testing.T) {
	registers := 0
	engine := reopenCatalogEngine(t, t.TempDir(), hongguoRegistrationTransport(&registers))
	client := engine.downloader.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", true
	if hongguoDevicesReady(t, engine, false) {
		t.Fatal("an unregistered helper device was reported ready")
	}
	if !hongguoDevicesReady(t, engine, true) || registers != 1 {
		t.Fatal("upgrade did not register only the helper device", registers)
	}
	if state := engine.downloader.hongguoCatalogSnapshot(); state.DeviceID != "7" || state.HelperDeviceID != "1001" {
		t.Fatal("upgrade replaced the registered primary device", state)
	}
}

func TestHongguoDevicesExplicitRetryIgnoresPause(t *testing.T) {
	registers := 0
	engine := reopenCatalogEngine(t, t.TempDir(), hongguoRegistrationTransport(&registers))
	client := engine.downloader.hongguoClient()
	client.registerPausedUntil, client.registerPauseErr = time.Now().Add(time.Hour), errors.New("紅果設備註冊未返回設備號")
	if !hongguoDevicesReady(t, engine, true) || registers != 2 {
		t.Fatal("explicit registration stayed paused", registers)
	}
}

func TestHongguoDevicesRepairsInvalidSavedHelperIdentity(t *testing.T) {
	registers := 0
	engine := reopenCatalogEngine(t, t.TempDir(), hongguoRegistrationTransport(&registers))
	client := engine.downloader.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", true
	client.state.HelperDeviceID, client.state.HelperInstallID, client.state.HelperRegistered = "invalid", "0", true
	if hongguoDevicesReady(t, engine, false) {
		t.Fatal("invalid saved helper identity was reported ready")
	}
	if !hongguoDevicesReady(t, engine, true) || registers != 1 {
		t.Fatal("invalid helper identity was not repaired independently", registers)
	}
	state := engine.downloader.hongguoCatalogSnapshot()
	if state.DeviceID != "7" || state.HelperDeviceID != "1001" || !state.HelperRegistered {
		t.Fatal("repair changed the primary device or failed to persist the helper", state)
	}
}
