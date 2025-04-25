// SPDX-License-Identifier: BUSL-1.1-or-later
// SPDX-FileCopyrightText: 2025 Web3 Technologies Inc. <https://asphere.xyz/>
// Copyright (c) 2025 Web3 Technologies Inc. All rights reserved.
// Use of this software is governed by the Business Source License included in the LICENSE file <https://github.com/Asphere-xyz/tacchain/blob/main/LICENSE>.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stretchr/testify/suite"
)

const (
	DefaultChainID        = "tacchain_2390-1"
	DefaultDenom          = "utac"
	DefaultKeyringBackend = "test"
)

type TacchainTestSuite struct {
	suite.Suite
	homeDir string
	cmd     *exec.Cmd
}

type CommandParams struct {
	ChainID        string
	HomeDir        string
	KeyringBackend string
}

func (s *TacchainTestSuite) CommandParamsHomeDir() CommandParams {
	return CommandParams{
		HomeDir: s.homeDir,
	}
}

func (s *TacchainTestSuite) CommandParamsChainIDHomeDir() CommandParams {
	return CommandParams{
		ChainID: DefaultChainID,
		HomeDir: s.homeDir,
	}
}

func (s *TacchainTestSuite) DefaultCommandParams() CommandParams {
	return CommandParams{
		ChainID:        DefaultChainID,
		HomeDir:        s.homeDir,
		KeyringBackend: DefaultKeyringBackend,
	}
}

func ExecuteBaseCommand(ctx context.Context, params CommandParams, args []string, isJSON bool) (string, error) {
	cmd := exec.CommandContext(ctx, "tacchaind", args...)
	cmd.Args = append(cmd.Args, "--home", params.HomeDir)

	if params.ChainID != "" {
		cmd.Args = append(cmd.Args, "--chain-id", params.ChainID)
	}

	if params.KeyringBackend != "" {
		cmd.Args = append(cmd.Args, "--keyring-backend", params.KeyringBackend)
	}

	if isJSON {
		cmd.Args = append(cmd.Args, "--output", "json")
	}

	output, err := cmd.CombinedOutput()
	strOutput := string(output)

	// NOTE: This Warning gets thrown on go 1.24 and gets applied to the output
	sonicWarning := "WARNING:(ast) sonic only supports go1.17~1.23, but your environment is not suitable\n"
	strOutput = strings.Replace(strOutput, sonicWarning, "", 1)

	return strOutput, err
}

func ExecuteCommand(ctx context.Context, params CommandParams, args ...string) (string, error) {
	return ExecuteBaseCommand(ctx, params, args, true)
}

func ExecuteAddressCommand(ctx context.Context, params CommandParams, args ...string) (string, error) {
	return ExecuteBaseCommand(ctx, params, args, false)
}

func GetAddress(ctx context.Context, s *TacchainTestSuite, keyName string) (string, error) {
	params := s.DefaultCommandParams()
	output, err := ExecuteAddressCommand(ctx, params, "keys", "show", keyName, "-a")
	if err != nil {
		return "", fmt.Errorf("failed to get %s address: %v", keyName, err)
	}
	return strings.TrimSpace(output), nil
}

func QueryBankBalances(ctx context.Context, s *TacchainTestSuite, address string) (string, error) {
	params := s.CommandParamsHomeDir()
	output, err := ExecuteCommand(ctx, params, "q", "bank", "balances", address)
	if err != nil {
		return "", fmt.Errorf("failed to query balance: %v", err)
	}
	return parseBalanceAmount(output), nil
}

func TxBankSend(ctx context.Context, s *TacchainTestSuite, from, to string, utacAmount string) (string, error) {
	params := s.DefaultCommandParams()
	output, err := ExecuteCommand(ctx, params, "tx", "bank", "send", from, to, utacAmount, "--gas", "200000", "-y")
	return output, err
}

type BlockResponse struct {
	Header struct {
		Height string `json:"height"`
	} `json:"header"`
}

type GenericResponse map[string]interface{}

type BalanceResponse struct {
	Balances []struct {
		Amount string `json:"amount"`
		Denom  string `json:"denom"`
	} `json:"balances"`
	DelegationResponse struct {
		Balance struct {
			Amount string `json:"amount"`
			Denom  string `json:"denom"`
		} `json:"balance"`
	} `json:"delegation_response"`
}

func parseBlockHeight(output string) int64 {
	var response BlockResponse
	//NOTE: In some outputs we have a sentence before the JSON object starts
	jsonStart := strings.Index(output, "{")
	if jsonStart > 0 {
		output = output[jsonStart:]
	}

	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return -1
	}

	height, err := strconv.ParseInt(response.Header.Height, 10, 64)
	if err != nil {
		return -1
	}
	return height
}

func parseField(output string, fieldName string) string {
	var response map[string]interface{}
	jsonStart := strings.Index(output, "{")
	if jsonStart > 0 {
		output = output[jsonStart:]
	}

	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return ""
	}

	// Try direct access first
	if value, exists := response[fieldName]; exists {
		return convertValueToString(value)
	}

	// Check params
	if params, exists := response["params"]; exists {
		if paramsMap, ok := params.(map[string]interface{}); ok {
			if value, exists := paramsMap[fieldName]; exists {
				return convertValueToString(value)
			}
		}
	}

	// Check validator
	if validator, exists := response["validator"]; exists {
		if validatorMap, ok := validator.(map[string]interface{}); ok {
			if value, exists := validatorMap[fieldName]; exists {
				return convertValueToString(value)
			}
		}
	}

	// NOTE: Special case for authority account
	// Check account -> value -> address
	if account, exists := response["account"]; exists {
		if accountMap, ok := account.(map[string]interface{}); ok {
			if value, exists := accountMap["value"]; exists {
				if valueMap, ok := value.(map[string]interface{}); ok {
					if address, exists := valueMap["address"]; exists {
						return convertValueToString(address)
					}
				}
			}
		}
	}

	return ""
}

func convertValueToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseBalanceAmount(output string) string {
	var response BalanceResponse
	jsonStart := strings.Index(output, "{")
	if jsonStart > 0 {
		output = output[jsonStart:]
	}

	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return "0" + DefaultDenom
	}

	// Check for delegation response first
	if response.DelegationResponse.Balance.Amount != "" {
		return response.DelegationResponse.Balance.Amount + response.DelegationResponse.Balance.Denom
	}

	// Fall back to regular balance response
	if len(response.Balances) > 0 {
		return response.Balances[0].Amount + response.Balances[0].Denom
	}

	return "0" + DefaultDenom
}

func killProcessOnPort(port int) error {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-t")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, pid := range pids {
		if pid == "" {
			continue
		}
		killCmd := exec.Command("kill", "-9", pid)
		if err := killCmd.Run(); err != nil {
			return fmt.Errorf("failed to kill process %s: %v", pid, err)
		}
	}
	return nil
}

func getCurrentBlockHeight(s *TacchainTestSuite) int64 {
	ctx := context.Background()
	params := s.CommandParamsHomeDir()
	output, err := ExecuteCommand(ctx, params, "q", "block")
	if err != nil {
		return -1
	}

	return parseBlockHeight(string(output))
}

func waitForNewBlock(s *TacchainTestSuite, stderr io.ReadCloser) {
	maxAttempts := 30
	attempt := 0

	initialHeight := getCurrentBlockHeight(s)

	for attempt < maxAttempts {
		currentHeight := getCurrentBlockHeight(s)
		if currentHeight > initialHeight {
			s.T().Logf("New block minted at height %d", currentHeight)
			return
		}

		attempt++
		if attempt == maxAttempts {
			if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
				errOutput, _ := io.ReadAll(stderr)
				s.T().Fatalf("Chain process exited unexpectedly: %s", string(errOutput))
			}
			s.T().Fatalf("Chain failed to produce new block after %d attempts", maxAttempts)
		}

		time.Sleep(3 * time.Second)
		s.T().Logf("Waiting for new block (attempt %d/%d)", attempt, maxAttempts)
	}
}

func UTacAmount(amount int64) string {
	return fmt.Sprintf("%d%s", amount, DefaultDenom)
}

func GetValidatorAddress(ctx context.Context, s *TacchainTestSuite) (string, error) {
	params := s.DefaultCommandParams()
	validatorAddr, err := ExecuteAddressCommand(ctx, params, "keys", "show", "validator", "--bech", "val", "-a")
	if err != nil {
		return "", fmt.Errorf("failed to query validator info: %v", err)
	}
	return strings.TrimSpace(validatorAddr), nil
}

func CreateFeemarketProposalFile(s *TacchainTestSuite, newBaseFee string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	params := s.CommandParamsHomeDir()
	output, err := ExecuteCommand(ctx, params, "q", "auth", "module-account", "gov")
	if err != nil {
		return "", fmt.Errorf("failed to get governance module address: %v", err)
	}

	governanceAddr := parseField(output, "address")
	if governanceAddr == "" {
		return "", fmt.Errorf("failed to extract governance module address")
	}

	proposalContent := fmt.Sprintf(`{ 
	"messages": [
		{
			"@type": "/cosmos.evm.feemarket.v1.MsgUpdateParams",
			"authority": "%s",
			"params": {
				"no_base_fee": false,
				"base_fee_change_denominator": 8,
				"elasticity_multiplier": 2,
				"enable_height": "0",
				"base_fee": "%s", 
				"min_gas_price": "0.000000000000000000",
				"min_gas_multiplier": "0.500000000000000000"
			}
		}
	],
	"metadata": "ipfs://CID",
	"deposit": "20000000utac",
	"title": "test",
	"summary": "test",
	"expedited": false
}`, governanceAddr, newBaseFee)

	proposalFile := filepath.Join(s.homeDir, "draft_proposal.json")
	err = os.WriteFile(proposalFile, []byte(proposalContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write proposal file: %v", err)
	}

	return proposalFile, nil
}
