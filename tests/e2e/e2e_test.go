package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestTacchainTestSuite(t *testing.T) {
	suite.Run(t, new(TacchainTestSuite))
}

func (s *TacchainTestSuite) TestChainInitialization() {
	genesisPath := filepath.Join(s.homeDir, "config", "genesis.json")
	_, err := os.Stat(genesisPath)
	require.NoError(s.T(), err, "Genesis file should exist")

	configFiles := []string{
		"config.toml",
		"app.toml",
		"client.toml",
	}

	for _, file := range configFiles {
		path := filepath.Join(s.homeDir, "config", file)
		_, err := os.Stat(path)
		require.NoError(s.T(), err, "Config file %s should exist", file)
	}
}

func (s *TacchainTestSuite) TestQueryBalances() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := s.CommandParamsHomeDir()
	output, err := DefaultExecuteCommand(ctx, params, "status")
	require.NoError(s.T(), err, "Failed to get status: %s", output)

	validatorAddr, err := GetAddress(ctx, s, "validator")
	require.NoError(s.T(), err, "Failed to get validator address")

	balance, err := QueryBankBalances(ctx, s, validatorAddr)
	require.NoError(s.T(), err, "Failed to query balances: %s", balance)
	require.Contains(s.T(), balance, DefaultDenom, "Balance should contain utac denomination")
}

func (s *TacchainTestSuite) TestBankSend() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := s.DefaultCommandParams()
	_, err := DefaultExecuteCommand(ctx, params, "keys", "add", "recipient")
	require.NoError(s.T(), err, "Failed to add recipient account")

	recipientAddr, err := GetAddress(ctx, s, "recipient")
	require.NoError(s.T(), err, "Failed to get recipient address")

	validatorAddr, err := GetAddress(ctx, s, "validator")
	require.NoError(s.T(), err, "Failed to get validator address")

	initialValidatorBalance, err := QueryBankBalances(ctx, s, validatorAddr)
	require.NoError(s.T(), err, "Failed to query validator balance")

	initialRecipientBalance, err := QueryBankBalances(ctx, s, recipientAddr)
	require.NoError(s.T(), err, "Failed to query recipient balance")

	amount := int64(1000000)
	err = TxBankSend(ctx, s, "validator", recipientAddr, amount)
	require.NoError(s.T(), err, "Failed to send tokens")

	checkIfNewBlockMinted(s, nil, true)

	finalValidatorBalance, err := QueryBankBalances(ctx, s, validatorAddr)
	require.NoError(s.T(), err, "Failed to query validator balance after tx")

	finalRecipientBalance, err := QueryBankBalances(ctx, s, recipientAddr)
	require.NoError(s.T(), err, "Failed to query recipient balance after tx")

	require.NotEqual(s.T(), initialValidatorBalance, finalValidatorBalance, "Validator balance should have changed")
	require.NotEqual(s.T(), initialRecipientBalance, finalRecipientBalance, "Recipient balance should have changed")
	require.Contains(s.T(), finalRecipientBalance, fmt.Sprintf("%d%s", amount, DefaultDenom), "Recipient should have received the sent amount")
}

func (s *TacchainTestSuite) TestInflationRate() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := s.CommandParamsHomeDir()
	output, err := DefaultExecuteCommand(ctx, params, "query", "mint", "params")
	require.NoError(s.T(), err, "Failed to query mint params: %s", output)

	inflationRateStr := parseField(output, "inflation_rate_change")
	require.NotEmpty(s.T(), inflationRateStr, "Inflation rate not found in mint params")

	inflationRate, err := strconv.ParseFloat(inflationRateStr, 64)
	require.NoError(s.T(), err, "Failed to parse inflation rate: %s", inflationRateStr)

	// Divide by 10^18 to convert from base units to percentage
	inflationRate = inflationRate / 1e18

	require.Greater(s.T(), inflationRate, 0.0, "Inflation rate should be positive")
	require.Less(s.T(), inflationRate, 0.20, "Inflation rate should be less than 20%")
}
