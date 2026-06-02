package config

import (
	"bytes"
	"testing"

	"github.com/megawron/lok8s/types"
)

func TestStore_ConfigMap(t *testing.T) {
	s := NewStore(nil)

	cm := types.ConfigMap{
		Metadata: types.ObjectMeta{
			Name:      "my-cm",
			Namespace: "default",
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	s.StoreConfigMap(cm)

	fetched, ok := s.LoadConfigMap("default", "my-cm")
	if !ok {
		t.Fatal("failed to load configmap")
	}

	if fetched.Data["key1"] != "value1" {
		t.Errorf("expected value1, got %s", fetched.Data["key1"])
	}

	list := s.ListConfigMaps("default")
	if len(list) != 1 || list[0].Metadata.Name != "my-cm" {
		t.Errorf("unexpected list result: %+v", list)
	}

	s.DeleteConfigMap("default", "my-cm")
	_, ok = s.LoadConfigMap("default", "my-cm")
	if ok {
		t.Error("configmap should have been deleted")
	}
}

func TestStore_Secret(t *testing.T) {
	s := NewStore(nil)

	sec := types.Secret{
		Metadata: types.ObjectMeta{
			Name:      "my-sec",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"sec1": []byte("val1"),
		},
		StringData: map[string]string{
			"sec2": "val2",
		},
	}

	s.StoreSecret(sec)

	fetched, ok := s.LoadSecret("default", "my-sec")
	if !ok {
		t.Fatal("failed to load secret")
	}

	// StringData should be merged into Data and cleared
	if !bytes.Equal(fetched.Data["sec1"], []byte("val1")) {
		t.Errorf("expected val1, got %s", fetched.Data["sec1"])
	}
	if !bytes.Equal(fetched.Data["sec2"], []byte("val2")) {
		t.Errorf("expected val2, got %s", fetched.Data["sec2"])
	}
	if len(fetched.StringData) != 0 {
		t.Errorf("stringData should be cleared, got %+v", fetched.StringData)
	}

	s.DeleteSecret("default", "my-sec")
	_, ok = s.LoadSecret("default", "my-sec")
	if ok {
		t.Error("secret should have been deleted")
	}
}
