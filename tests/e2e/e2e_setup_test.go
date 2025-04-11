package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

func (s *TacchainTestSuite) SetupSuite() {
	s.T().Log("Setting up test suite...")

	if err := killProcessOnPort(26657); err != nil {
		s.T().Logf("Warning: Failed to kill process on port 26657: %v", err)
	}

	dir, err := os.MkdirTemp("", "tacchain-test")
	if err != nil {
		s.T().Fatalf("Failed to create temporary directory: %v", err)
	}
	s.homeDir = dir

	if err := s.initChain(); err != nil {
		s.T().Fatalf("Failed to initialize chain: %v", err)
	}
	if err := s.startChain(); err != nil {
		s.T().Fatalf("Failed to start chain: %v", err)
	}
}

func (s *TacchainTestSuite) initChain() error {
	s.T().Log("Initializing chain...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := s.DefaultCommandParams()

	_, err := DefaultExecuteCommand(ctx, params, "init", "test", "--default-denom", DefaultDenom)
	if err != nil {
		return fmt.Errorf("failed to initialize chain: %v", err)
	}

	_, err = DefaultExecuteCommand(ctx, params, "keys", "add", "validator")
	if err != nil {
		return fmt.Errorf("failed to add validator key: %v", err)
	}

	genesisAmount := fmt.Sprintf("1000000000000000000000000000000%s", DefaultDenom)
	_, err = DefaultExecuteCommand(ctx, params, "genesis", "add-genesis-account", "validator", genesisAmount)
	if err != nil {
		return fmt.Errorf("failed to add genesis account: %v", err)
	}

	gentxAmount := fmt.Sprintf("10000000000000000000000000000%s", DefaultDenom)
	_, err = DefaultExecuteCommand(ctx, params, "genesis", "gentx", "validator", gentxAmount)
	if err != nil {
		return fmt.Errorf("failed to create gentx: %v", err)
	}

	_, err = DefaultExecuteCommand(ctx, params, "genesis", "collect-gentxs")
	if err != nil {
		return fmt.Errorf("failed to collect gentxs: %v", err)
	}

	return nil
}

func (s *TacchainTestSuite) startChain() error {
	s.T().Log("Starting chain process...")

	s.cmd = exec.Command("tacchaind", "start", "--chain-id", DefaultChainID, "--home", s.homeDir)

	stderr, err := s.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	err = s.cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start chain: %v", err)
	}

	s.T().Log("Waiting for chain to start producing blocks...")

	checkIfNewBlockMinted(s, stderr)

	if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
		errOutput, _ := io.ReadAll(stderr)
		return fmt.Errorf("chain process exited unexpectedly: %s", string(errOutput))
	}

	return nil
}

func (s *TacchainTestSuite) TearDownSuite() {
	s.T().Log("Tearing down Tacchain test suite...")

	if s.cmd != nil {
		s.T().Log("Stopping chain process...")
		if err := s.cmd.Process.Kill(); err != nil {
			s.T().Logf("Error stopping chain process: %v", err)
		}
		s.cmd.Wait()
	}

	if err := os.RemoveAll(s.homeDir); err != nil {
		s.T().Logf("Error cleaning up test directory: %v", err)
	}
}
