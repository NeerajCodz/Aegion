package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewService(t *testing.T) {
	cfg := Config{BootstrapEnabled: true}

	svc := New(nil, cfg)

	assert.NotNil(t, svc)
	assert.Equal(t, cfg, svc.config)
	assert.Nil(t, svc.store)
}

func TestValidRolesMap(t *testing.T) {
	assert.True(t, ValidRoles["super_admin"])
	assert.True(t, ValidRoles["admin"])
	assert.True(t, ValidRoles["operator"])
	assert.True(t, ValidRoles["viewer"])
	assert.False(t, ValidRoles["unknown"])
}
