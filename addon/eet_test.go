package addon_test

import (
	"testing"

	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/head"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddonRegistered(t *testing.T) {
	def := tax.AddonForKey(addon.V1)
	require.NotNil(t, def)
	assert.Equal(t, "Czech Republic EET 2.0", def.Name.In(i18n.EN))
}

func TestDefinitionsRegistered(t *testing.T) {
	for _, key := range []cbc.Key{addon.ExtKeyUnit, addon.ExtKeyDevice} {
		ext := tax.ExtensionForKey(key)
		require.NotNil(t, ext, key)
		assert.Equal(t, []cbc.Key{addon.V1}, ext.Usage.Keys)
	}
	stamp := head.StampDefFor(addon.StampPOK)
	require.NotNil(t, stamp)
	assert.Equal(t, "EET: Confirmation Code (POK)", stamp.Name.In(i18n.EN))
}
