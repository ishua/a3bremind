package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAddCommand_Series(t *testing.T) {
	label, repeat, times, minGap, err := parseAddCommand(`/add "Капли" daily 07:00 11:00 15:00`)
	assert.NoError(t, err)
	assert.Equal(t, "Капли", label)
	assert.Equal(t, "daily", repeat)
	assert.Equal(t, []string{"07:00", "11:00", "15:00"}, times)
	assert.Nil(t, minGap)
}

func TestParseAddCommand_WithGap(t *testing.T) {
	label, repeat, times, minGap, err := parseAddCommand(`/add "Капли" daily gap:3h 07:00 11:00 15:00`)
	assert.NoError(t, err)
	assert.Equal(t, "Капли", label)
	assert.Equal(t, "daily", repeat)
	assert.Equal(t, []string{"07:00", "11:00", "15:00"}, times)
	require.NotNil(t, minGap)
	assert.Equal(t, 180, *minGap)
}

func TestParseAddCommand_GapMinutes(t *testing.T) {
	label, repeat, times, minGap, err := parseAddCommand(`/add "Капли" daily gap:30m 07:00 11:00`)
	assert.NoError(t, err)
	assert.Equal(t, "Капли", label)
	assert.Equal(t, "daily", repeat)
	assert.Equal(t, []string{"07:00", "11:00"}, times)
	require.NotNil(t, minGap)
	assert.Equal(t, 30, *minGap)
}

func TestParseAddCommand_Single(t *testing.T) {
	label, repeat, times, minGap, err := parseAddCommand(`/add "Test" daily 09:00`)
	assert.NoError(t, err)
	assert.Equal(t, "Test", label)
	assert.Equal(t, "daily", repeat)
	assert.Equal(t, []string{"09:00"}, times)
	assert.Nil(t, minGap)
}

func TestParseAddCommand_InvalidGap(t *testing.T) {
	_, _, _, _, err := parseAddCommand(`/add "Test" daily gap:xyz 09:00`)
	assert.Error(t, err)
}

func TestParseAddCommand_InvalidTime(t *testing.T) {
	_, _, _, _, err := parseAddCommand(`/add "Test" daily 25:00`)
	assert.Error(t, err)
}

func TestParseAddCommand_NoTimes(t *testing.T) {
	_, _, _, _, err := parseAddCommand(`/add "Test" daily`)
	assert.Error(t, err)
}

func TestParseAddCommand_Once(t *testing.T) {
	label, repeat, times, minGap, err := parseAddCommand(`/add "Once" once 09:00`)
	assert.NoError(t, err)
	assert.Equal(t, "Once", label)
	assert.Equal(t, "once", repeat)
	assert.Equal(t, []string{"09:00"}, times)
	assert.Nil(t, minGap)
}
