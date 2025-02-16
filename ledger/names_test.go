package ledger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExample(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(CrouchingTigerName([]byte("12345")).String(), "calculating-meerkat-straight-beetle")
}
