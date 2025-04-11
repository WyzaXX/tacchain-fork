package e2e

import (
	"context"
	"fmt"
	"io"
	"os/exec"
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

func (s *TacchainTestSuite) DefaultCommandParams() CommandParams {
	return CommandParams{
		ChainID:        DefaultChainID,
		HomeDir:        s.homeDir,
		KeyringBackend: DefaultKeyringBackend,
	}
}

func (s *TacchainTestSuite) CommandParamsChainIDHomeDir() CommandParams {
	return CommandParams{
		ChainID: DefaultChainID,
		HomeDir: s.homeDir,
	}
}

func (s *TacchainTestSuite) CommandParamsHomeDir() CommandParams {
	return CommandParams{
		HomeDir: s.homeDir,
	}
}

func DefaultExecuteCommand(ctx context.Context, params CommandParams, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tacchaind", args...)

	cmd.Args = append(cmd.Args, "--home", params.HomeDir)

	if params.ChainID != "" {
		cmd.Args = append(cmd.Args, "--chain-id", params.ChainID)
	}

	if params.KeyringBackend != "" {
		cmd.Args = append(cmd.Args, "--keyring-backend", params.KeyringBackend)
	}

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func GetAddress(ctx context.Context, s *TacchainTestSuite, keyName string) (string, error) {
	params := s.DefaultCommandParams()
	output, err := DefaultExecuteCommand(ctx, params, "keys", "show", keyName, "-a")
	if err != nil {
		return "", fmt.Errorf("failed to get %s address: %v", keyName, err)
	}
	return strings.TrimSpace(output), nil
}

func QueryBankBalances(ctx context.Context, s *TacchainTestSuite, address string) (string, error) {
	params := s.CommandParamsHomeDir()
	output, err := DefaultExecuteCommand(ctx, params, "query", "bank", "balances", address)
	if err != nil {
		return "", fmt.Errorf("failed to query balance: %v", err)
	}
	return parseBalanceAmount(output), nil
}

func TxBankSend(ctx context.Context, s *TacchainTestSuite, from, to string, amount int64) error {
	params := s.DefaultCommandParams()
	amountWithDenom := fmt.Sprintf("%d%s", amount, DefaultDenom)
	_, err := DefaultExecuteCommand(ctx, params, "tx", "bank", "send", from, to, amountWithDenom, "-y")
	return err
}

func parseField(output string, fieldName string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if strings.Contains(line, fieldName+":") {
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), "\"")
			}
		}
	}
	return ""
}

func GetBlockHeight(ctx context.Context, s *TacchainTestSuite) (int64, error) {
	params := s.DefaultCommandParams()
	output, err := DefaultExecuteCommand(ctx, params, "q", "block")
	if err != nil {
		return -1, err
	}
	heightStr := parseField(output, "height")
	if heightStr == "" {
		return -1, fmt.Errorf("height not found in block output")
	}
	return strconv.ParseInt(heightStr, 10, 64)
}

func parseBalanceAmount(balanceOutput string) string {
	lines := strings.Split(balanceOutput, "\n")
	var amount, denom string
	for _, line := range lines {
		if strings.Contains(line, "amount:") {
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) == 2 {
				amount = strings.Trim(strings.TrimSpace(parts[1]), "\"")
			}
		}
		if strings.Contains(line, "denom:") {
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) == 2 {
				denom = strings.Trim(strings.TrimSpace(parts[1]), "\"")
			}
		}
	}
	if amount == "" {
		return "0" + DefaultDenom
	}
	return amount + denom
}

func killProcessOnPort(port int) error {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-t")
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("netstat", "-ano", "-p", "tcp")
		output, err = cmd.Output()
		if err != nil {
			fmt.Printf("Warning: could not check for processes on port %d: %v\n", port, err)
			return nil
		}

		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, fmt.Sprintf(":%d", port)) {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					pid := fields[len(fields)-1]
					killCmd := exec.Command("kill", "-9", pid)
					if err := killCmd.Run(); err != nil {
						fmt.Printf("Warning: failed to kill process %s: %v\n", pid, err)
					}
					fmt.Printf("Killed process %s on port %d\n", pid, port)
				}
			}
		}
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
	cmd := exec.Command("tacchaind", "q", "block", "--home", s.homeDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return -1
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "height:") {
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) == 2 {
				heightStr := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				height, err := strconv.ParseInt(heightStr, 10, 64)
				if err == nil {
					return height
				}
			}
		}
	}
	return -1
}

func checkIfNewBlockMinted(s *TacchainTestSuite, stderr io.ReadCloser, waitForNewBlock ...bool) {
	maxAttempts := 30
	attempt := 0
	waitForNext := len(waitForNewBlock) > 0 && waitForNewBlock[0]

	var initialHeight int64 = -1
	if waitForNext {
		initialHeight = getCurrentBlockHeight(s)
		if initialHeight == -1 {
			s.T().Fatalf("Failed to get initial block height")
		}
		s.T().Logf("Waiting for block height to increase from %d", initialHeight)
	}

	for attempt < maxAttempts {
		cmd := exec.Command("tacchaind", "q", "block", "--home", s.homeDir)
		output, err := cmd.CombinedOutput()

		if err == nil && strings.Contains(string(output), "height:") {
			if waitForNext {
				currentHeight := getCurrentBlockHeight(s)
				if currentHeight > initialHeight {
					s.T().Logf("New block minted at height %d", currentHeight)
					break
				}
			} else {
				s.T().Log("Chain is producing blocks")
				break
			}
		}

		attempt++
		if attempt == maxAttempts {
			if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
				errOutput, _ := io.ReadAll(stderr)
				s.T().Fatalf("Chain process exited unexpectedly: %s", string(errOutput))
			}
			s.T().Fatalf("Chain failed to produce blocks after %d attempts", maxAttempts)
		}

		time.Sleep(1 * time.Second)
	}
}
