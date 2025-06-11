package cryptovote

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed crypto_vote.wasm
var wasmFile []byte

// KeyPair represents a cryptographic key pair with secret and public keys encoded as base64
type KeyPair struct {
	SecretKey string `json:"secret_key"`
	PublicKey string `json:"public_key"`
}

// CryptoVote provides access to the WASM cryptographic functions
type CryptoVote struct {
	runtime wazero.Runtime
	ctx     context.Context
	module  api.Module

	// WASM function exports
	allocFunc          api.Function
	freeFunc           api.Function
	genMixnetKeyPair   api.Function
	genTrusteeKeyPair  api.Function
	encryptFunc        api.Function
	decryptMixnetFunc  api.Function
	decryptTrusteeFunc api.Function
	cypherSizeFunc     api.Function

	memory api.Memory
}

// NewCryptoVote creates a new CryptoVote instance and initializes the WASM module
func NewCryptoVote(ctx context.Context) (*CryptoVote, error) {
	// Create a new WASM runtime
	runtime := wazero.NewRuntime(ctx)

	// Instantiate WASI, which implements the WebAssembly System Interface
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	// Define host functions that the WASM module expects
	hostModule, err := runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(consoleLog).
		Export("console_log").
		NewFunctionBuilder().
		WithFunc(getRandom).
		Export("get_random").
		Instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate host module: %w", err)
	}
	defer hostModule.Close(ctx)

	// Instantiate the WASM module
	module, err := runtime.InstantiateWithConfig(ctx, wasmFile, wazero.NewModuleConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	// Get function exports
	allocFunc := module.ExportedFunction("alloc")
	if allocFunc == nil {
		return nil, fmt.Errorf("alloc function not found in WASM module")
	}

	freeFunc := module.ExportedFunction("free")
	if freeFunc == nil {
		return nil, fmt.Errorf("free function not found in WASM module")
	}

	genMixnetKeyPair := module.ExportedFunction("gen_mixnet_key_pair")
	if genMixnetKeyPair == nil {
		return nil, fmt.Errorf("gen_mixnet_key_pair function not found in WASM module")
	}

	genTrusteeKeyPair := module.ExportedFunction("gen_trustee_key_pair")
	if genTrusteeKeyPair == nil {
		return nil, fmt.Errorf("gen_trustee_key_pair function not found in WASM module")
	}

	encryptFunc := module.ExportedFunction("encrypt")
	if encryptFunc == nil {
		return nil, fmt.Errorf("encrypt function not found in WASM module")
	}

	decryptMixnetFunc := module.ExportedFunction("decrypt_mixnet")
	if decryptMixnetFunc == nil {
		return nil, fmt.Errorf("decrypt_mixnet function not found in WASM module")
	}

	decryptTrusteeFunc := module.ExportedFunction("decrypt_trustee")
	if decryptTrusteeFunc == nil {
		return nil, fmt.Errorf("decrypt_trustee function not found in WASM module")
	}

	cypherSizeFunc := module.ExportedFunction("cypher_size")
	if cypherSizeFunc == nil {
		return nil, fmt.Errorf("cypher_size function not found in WASM module")
	}

	// Get memory export
	memory := module.ExportedMemory("memory")
	if memory == nil {
		return nil, fmt.Errorf("memory not found in WASM module")
	}

	return &CryptoVote{
		runtime:            runtime,
		ctx:                ctx,
		module:             module,
		allocFunc:          allocFunc,
		freeFunc:           freeFunc,
		genMixnetKeyPair:   genMixnetKeyPair,
		genTrusteeKeyPair:  genTrusteeKeyPair,
		encryptFunc:        encryptFunc,
		decryptMixnetFunc:  decryptMixnetFunc,
		decryptTrusteeFunc: decryptTrusteeFunc,
		cypherSizeFunc:     cypherSizeFunc,
		memory:             memory,
	}, nil
}

// Close releases resources used by the CryptoVote instance
func (cv *CryptoVote) Close() error {
	if cv.module != nil {
		if err := cv.module.Close(cv.ctx); err != nil {
			return err
		}
	}
	if cv.runtime != nil {
		if err := cv.runtime.Close(cv.ctx); err != nil {
			return err
		}
	}
	return nil
}

// Host function: console_log - logs messages from WASM
func consoleLog(ctx context.Context, m api.Module, ptr, len uint32) {
	if memory := m.ExportedMemory("memory"); memory != nil {
		if data, ok := memory.Read(ptr, len); ok {
			log.Printf("WASM CryptoVote Module: %s", string(data))
		}
	}
}

// Host function: get_random - provides random bytes to WASM
func getRandom(ctx context.Context, m api.Module, ptr, amount uint32) {
	if memory := m.ExportedMemory("memory"); memory != nil {
		data := make([]byte, amount)
		if _, err := rand.Read(data); err == nil {
			memory.Write(ptr, data)
		}
	}
}

// allocate memory in WASM
func (cv *CryptoVote) alloc(size uint32) (uint32, error) {
	results, err := cv.allocFunc.Call(cv.ctx, uint64(size))
	if err != nil {
		return 0, err
	}
	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, fmt.Errorf("allocation failed")
	}
	return ptr, nil
}

// free memory in WASM
func (cv *CryptoVote) free(ptr, size uint32) error {
	_, err := cv.freeFunc.Call(cv.ctx, uint64(ptr), uint64(size))
	return err
}

// copyToWasm copies data from Go to WASM memory
func (cv *CryptoVote) copyToWasm(data []byte) (uint32, uint32, error) {
	size := uint32(len(data))
	ptr, err := cv.alloc(size)
	if err != nil {
		return 0, 0, err
	}

	if !cv.memory.Write(ptr, data) {
		cv.free(ptr, size)
		return 0, 0, fmt.Errorf("failed to write data to WASM memory")
	}

	return ptr, size, nil
}

// copyFromWasm copies data from WASM memory to Go
func (cv *CryptoVote) copyFromWasm(ptr, size uint32) ([]byte, error) {
	data, ok := cv.memory.Read(ptr, size)
	if !ok {
		return nil, fmt.Errorf("failed to read data from WASM memory")
	}

	result := make([]byte, size)
	copy(result, data)
	return result, nil
}

// readSizedBuffer reads a buffer with a 4-byte size prefix from WASM memory
func (cv *CryptoVote) readSizedBuffer(ptr uint32) ([]byte, error) {
	if ptr == 0 {
		return nil, fmt.Errorf("null pointer")
	}

	// Read the size (first 4 bytes)
	sizeData, ok := cv.memory.Read(ptr, 4)
	if !ok {
		return nil, fmt.Errorf("failed to read size from WASM memory")
	}

	size := binary.LittleEndian.Uint32(sizeData)

	// Read the actual data
	data, err := cv.copyFromWasm(ptr+4, size)
	if err != nil {
		cv.free(ptr, size+4) // Try to free even on error
		return nil, err
	}

	// Free the WASM memory
	if err := cv.free(ptr, size+4); err != nil {
		return nil, fmt.Errorf("failed to free WASM memory: %w", err)
	}

	return data, nil
}

// copyKeysToWasm copies base64-encoded keys to WASM memory
func (cv *CryptoVote) copyKeysToWasm(keys []string) (uint32, uint32, error) {
	if len(keys) == 0 {
		return 0, 0, fmt.Errorf("key list must not be empty")
	}

	// Each key is 32 bytes
	totalSize := uint32(len(keys) * 32)
	ptr, err := cv.alloc(totalSize)
	if err != nil {
		return 0, 0, err
	}

	for i, keyBase64 := range keys {
		keyBytes, err := base64.RawStdEncoding.DecodeString(keyBase64)
		if err != nil {
			cv.free(ptr, totalSize)
			return 0, 0, fmt.Errorf("failed to decode key %s from base64: %w", keyBase64, err)
		}

		if len(keyBytes) != 32 {
			cv.free(ptr, totalSize)
			return 0, 0, fmt.Errorf("key %d must be 32 bytes, got %d", i, len(keyBytes))
		}

		offset := ptr + uint32(i*32)
		if !cv.memory.Write(offset, keyBytes) {
			cv.free(ptr, totalSize)
			return 0, 0, fmt.Errorf("failed to write key %d to WASM memory", i)
		}
	}

	return ptr, uint32(len(keys)), nil
}

// GenMixnetKeyPair generates a new mixnet key pair
func (cv *CryptoVote) GenMixnetKeyPair() (*KeyPair, error) {
	results, err := cv.genMixnetKeyPair.Call(cv.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to call gen_mixnet_key_pair: %w", err)
	}

	ptr := uint32(results[0])
	if ptr == 0 {
		return nil, fmt.Errorf("failed to generate mixnet key pair")
	}

	// Copy the 64 bytes (32 secret + 32 public)
	keyData, err := cv.copyFromWasm(ptr, 64)
	if err != nil {
		return nil, err
	}

	// Free the WASM memory
	if err := cv.free(ptr, 64); err != nil {
		return nil, err
	}

	secretKey := base64.RawStdEncoding.EncodeToString(keyData[:32])
	publicKey := base64.RawStdEncoding.EncodeToString(keyData[32:64])

	return &KeyPair{
		SecretKey: secretKey,
		PublicKey: publicKey,
	}, nil
}

// GenTrusteeKeyPair generates a new trustee key pair
func (cv *CryptoVote) GenTrusteeKeyPair() (*KeyPair, error) {
	results, err := cv.genTrusteeKeyPair.Call(cv.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to call gen_trustee_key_pair: %w", err)
	}

	ptr := uint32(results[0])
	if ptr == 0 {
		return nil, fmt.Errorf("failed to generate trustee key pair")
	}

	// Copy the 64 bytes (32 secret + 32 public)
	keyData, err := cv.copyFromWasm(ptr, 64)
	if err != nil {
		return nil, err
	}

	// Free the WASM memory
	if err := cv.free(ptr, 64); err != nil {
		return nil, err
	}

	secretKey := base64.RawStdEncoding.EncodeToString(keyData[:32])
	publicKey := base64.RawStdEncoding.EncodeToString(keyData[32:64])

	return &KeyPair{
		SecretKey: secretKey,
		PublicKey: publicKey,
	}, nil
}

// EncryptResult represents the result of encryption with cyphers and control data
type EncryptResult struct {
	Cyphers     [2][]byte `json:"cyphers"`
	ControlData []byte    `json:"control_data"`
}

// Encrypt encrypts a message using mixnet and trustee public keys
func (cv *CryptoVote) Encrypt(mixnetPublicKeys []string, trusteePublicKeys []string, message string, maxSize uint32) (*EncryptResult, error) {
	if len(message) == 0 {
		return nil, fmt.Errorf("message must not be empty")
	}

	if uint32(len(message)) > maxSize {
		return nil, fmt.Errorf("message is bigger than max_size")
	}

	// Prepare message data
	messageBytes := []byte(message)
	msgPtr, msgSize, err := cv.copyToWasm(messageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to copy message to WASM: %w", err)
	}

	// Prepare mixnet keys
	mixnetKeysPtr, mixnetCount, err := cv.copyKeysToWasm(mixnetPublicKeys)
	if err != nil {
		cv.free(msgPtr, msgSize)
		return nil, fmt.Errorf("failed to copy mixnet keys to WASM: %w", err)
	}

	// Prepare trustee keys
	trusteeKeysPtr, trusteeCount, err := cv.copyKeysToWasm(trusteePublicKeys)
	if err != nil {
		cv.free(msgPtr, msgSize)
		cv.free(mixnetKeysPtr, mixnetCount*32)
		return nil, fmt.Errorf("failed to copy trustee keys to WASM: %w", err)
	}

	// Call WASM encrypt function
	results, err := cv.encryptFunc.Call(cv.ctx,
		uint64(mixnetCount),
		uint64(trusteeCount),
		uint64(mixnetKeysPtr),
		uint64(trusteeKeysPtr),
		uint64(msgPtr),
		uint64(msgSize),
		uint64(maxSize),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to call encrypt: %w", err)
	}

	resultPtr := uint32(results[0])
	if resultPtr == 0 {
		return nil, fmt.Errorf("encryption failed")
	}

	// Read the result (sized buffer)
	encryptedData, err := cv.readSizedBuffer(resultPtr)
	if err != nil {
		return nil, err
	}

	// Get cypher size to split the result
	cypherSizeResults, err := cv.cypherSizeFunc.Call(cv.ctx,
		uint64(len(mixnetPublicKeys)),
		uint64(maxSize),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cypher size: %w", err)
	}

	cypherSize := uint32(cypherSizeResults[0])

	// Split the data into cyphers and control data
	if len(encryptedData) < int(cypherSize*2) {
		return nil, fmt.Errorf("encrypted data too small: expected at least %d bytes, got %d", cypherSize*2, len(encryptedData))
	}

	cypher1 := make([]byte, cypherSize)
	cypher2 := make([]byte, cypherSize)
	copy(cypher1, encryptedData[0:cypherSize])
	copy(cypher2, encryptedData[cypherSize:cypherSize*2])

	controlData := make([]byte, len(encryptedData)-int(cypherSize*2))
	copy(controlData, encryptedData[cypherSize*2:])

	return &EncryptResult{
		Cyphers:     [2][]byte{cypher1, cypher2},
		ControlData: controlData,
	}, nil
}

// DecryptMixnet decrypts a block of ciphers with a mixnet private key
func (cv *CryptoVote) DecryptMixnet(secretKey string, cypherBlock []byte, cypherCount uint32) ([]byte, error) {
	// Decode secret key from base64
	keyBytes, err := base64.RawStdEncoding.DecodeString(secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode secret key from base64: %w", err)
	}

	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(keyBytes))
	}

	// Copy secret key to WASM
	keyPtr, err := cv.alloc(32)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory for secret key: %w", err)
	}

	if !cv.memory.Write(keyPtr, keyBytes) {
		cv.free(keyPtr, 32)
		return nil, fmt.Errorf("failed to write secret key to WASM memory")
	}

	// Copy cypher block to WASM
	cypherPtr, cypherSize, err := cv.copyToWasm(cypherBlock)
	if err != nil {
		cv.free(keyPtr, 32)
		return nil, fmt.Errorf("failed to copy cypher block to WASM: %w", err)
	}

	// Call WASM decrypt_mixnet function
	results, err := cv.decryptMixnetFunc.Call(cv.ctx,
		uint64(keyPtr),
		uint64(cypherCount),
		uint64(cypherPtr),
		uint64(cypherSize),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to call decrypt_mixnet: %w", err)
	}

	resultPtr := uint32(results[0])
	if resultPtr == 0 {
		return nil, fmt.Errorf("mixnet decryption failed")
	}

	// Read the result (sized buffer)
	return cv.readSizedBuffer(resultPtr)
}

// DecryptTrustee decrypts a block of ciphers with all trustee private keys
func (cv *CryptoVote) DecryptTrustee(secretKeys []string, cypherBlock []byte, cypherCount int) ([]string, error) {
	// Prepare trustee secret keys
	trusteeKeysPtr, trusteeCount, err := cv.copyKeysToWasm(secretKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to copy trustee keys to WASM: %w", err)
	}

	// Copy cypher block to WASM
	cypherPtr, cypherSize, err := cv.copyToWasm(cypherBlock)
	if err != nil {
		cv.free(trusteeKeysPtr, trusteeCount*32)
		return nil, fmt.Errorf("failed to copy cypher block to WASM: %w", err)
	}

	// Call WASM decrypt_trustee function
	results, err := cv.decryptTrusteeFunc.Call(cv.ctx,
		uint64(trusteeCount),
		uint64(trusteeKeysPtr),
		uint64(cypherCount),
		uint64(cypherPtr),
		uint64(cypherSize),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to call decrypt_trustee: %w", err)
	}

	resultPtr := uint32(results[0])
	if resultPtr == 0 {
		return nil, fmt.Errorf("trustee decryption failed")
	}

	// Read the result (sized buffer)
	decryptedData, err := cv.readSizedBuffer(resultPtr)
	if err != nil {
		return nil, err
	}

	messageSize := len(decryptedData) / int(cypherCount)
	messages := make([]string, cypherCount)

	for i := range cypherCount {
		start := int(i) * messageSize
		end := start + messageSize
		messageBytes := truncateAtNull(decryptedData[start:end])

		// Convert to string, handling empty/null-only data
		if len(messageBytes) == 0 {
			messages[i] = ""
		} else {
			messages[i] = string(messageBytes)
		}
	}

	return messages, nil
}

func truncateAtNull(data []byte) []byte {
	if index := bytes.IndexByte(data, 0); index != -1 {
		return data[:index]
	}
	return data
}
