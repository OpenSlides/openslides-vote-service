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
	validateFunc       api.Function

	memory api.Memory
}

// WasmBuffer represents a buffer in WASM memory with automatic cleanup
type WasmBuffer struct {
	cv   *CryptoVote
	ptr  uint32
	size uint32
}

// NewWasmBuffer creates a new buffer in WASM memory
func (cv *CryptoVote) NewWasmBuffer(size uint32) (*WasmBuffer, error) {
	if size == 0 {
		return nil, fmt.Errorf("buffer size cannot be zero")
	}
	if size > 10*1024*1024 { // 10MB safety limit
		return nil, fmt.Errorf("buffer size too large: %d bytes", size)
	}

	ptr, err := cv.alloc(size)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate WASM memory: %w", err)
	}

	return &WasmBuffer{
		cv:   cv,
		ptr:  ptr,
		size: size,
	}, nil
}

// Free releases the WASM memory buffer
func (wb *WasmBuffer) Free() error {
	if wb.ptr != 0 {
		err := wb.cv.free(wb.ptr, wb.size)
		wb.ptr = 0
		wb.size = 0
		return err
	}
	return nil
}

// Ptr returns the pointer to the WASM memory
func (wb *WasmBuffer) Ptr() uint32 {
	return wb.ptr
}

// Size returns the size of the buffer
func (wb *WasmBuffer) Size() uint32 {
	return wb.size
}

// Write writes data to the WASM buffer
func (wb *WasmBuffer) Write(data []byte) error {
	if uint32(len(data)) > wb.size {
		return fmt.Errorf("data too large for buffer: %d > %d", len(data), wb.size)
	}
	if !wb.cv.memory.Write(wb.ptr, data) {
		return fmt.Errorf("can not write wasm memory")
	}
	return nil
}

// Read reads data from the WASM buffer
func (wb *WasmBuffer) Read() ([]byte, error) {
	data, ok := wb.cv.memory.Read(wb.ptr, wb.size)
	if !ok {
		return nil, fmt.Errorf("failed to read from WASM memory")
	}
	return data, nil
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
		runtime.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate host module: %w", err)
	}
	defer hostModule.Close(ctx)

	// Instantiate the WASM module
	module, err := runtime.InstantiateWithConfig(ctx, wasmFile, wazero.NewModuleConfig())
	if err != nil {
		runtime.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	// Get function exports
	allocFunc := module.ExportedFunction("alloc")
	if allocFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("alloc function not found in WASM module")
	}

	freeFunc := module.ExportedFunction("free")
	if freeFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("free function not found in WASM module")
	}

	genMixnetKeyPair := module.ExportedFunction("gen_mixnet_key_pair")
	if genMixnetKeyPair == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("gen_mixnet_key_pair function not found in WASM module")
	}

	genTrusteeKeyPair := module.ExportedFunction("gen_trustee_key_pair")
	if genTrusteeKeyPair == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("gen_trustee_key_pair function not found in WASM module")
	}

	encryptFunc := module.ExportedFunction("encrypt")
	if encryptFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("encrypt function not found in WASM module")
	}

	decryptMixnetFunc := module.ExportedFunction("decrypt_mixnet")
	if decryptMixnetFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("decrypt_mixnet function not found in WASM module")
	}

	decryptTrusteeFunc := module.ExportedFunction("decrypt_trustee")
	if decryptTrusteeFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("decrypt_trustee function not found in WASM module")
	}

	cypherSizeFunc := module.ExportedFunction("cypher_size")
	if cypherSizeFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("cypher_size function not found in WASM module")
	}

	validateFunc := module.ExportedFunction("validate")
	if validateFunc == nil {
		module.Close(ctx)
		runtime.Close(ctx)
		return nil, fmt.Errorf("validate function not found in WASM module")
	}

	// Get memory export
	memory := module.ExportedMemory("memory")
	if memory == nil {
		module.Close(ctx)
		runtime.Close(ctx)
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
		validateFunc:       validateFunc,
		memory:             memory,
	}, nil
}

// Close releases resources used by the CryptoVote instance
func (cv *CryptoVote) Close() error {
	var errs []error

	if cv.module != nil {
		if err := cv.module.Close(cv.ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close module: %w", err))
		}
	}
	if cv.runtime != nil {
		if err := cv.runtime.Close(cv.ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close runtime: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
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

// alloc allocates memory in WASM
func (cv *CryptoVote) alloc(size uint32) (uint32, error) {
	results, err := cv.allocFunc.Call(cv.ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("alloc call failed: %w", err)
	}
	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, fmt.Errorf("allocation failed: returned null pointer")
	}
	return ptr, nil
}

// free frees memory in WASM
func (cv *CryptoVote) free(ptr, size uint32) error {
	if ptr == 0 {
		return nil // Nothing to free
	}
	_, err := cv.freeFunc.Call(cv.ctx, uint64(ptr), uint64(size))
	if err != nil {
		return fmt.Errorf("free call failed: %w", err)
	}
	return nil
}

// validateKeyList validates a list of base64-encoded keys
func validateKeyList(keys []string, name string) error {
	if len(keys) == 0 {
		return fmt.Errorf("%s cannot be empty", name)
	}
	if len(keys) > 1000 {
		return fmt.Errorf("%s too large: %d keys", name, len(keys))
	}

	for i, key := range keys {
		if key == "" {
			return fmt.Errorf("%s[%d] cannot be empty", name, i)
		}
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return fmt.Errorf("%s[%d] invalid base64: %w", name, i, err)
		}
		if len(decoded) != 32 {
			return fmt.Errorf("%s[%d] must be 32 bytes, got %d", name, i, len(decoded))
		}
	}
	return nil
}

// copyKeysToWasm copies a list of base64-encoded keys to WASM memory
func (cv *CryptoVote) copyKeysToWasm(keys []string) (*WasmBuffer, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("key list cannot be empty")
	}

	// Create buffer for all keys (32 bytes each)
	buffer, err := cv.NewWasmBuffer(uint32(len(keys) * 32))
	if err != nil {
		return nil, fmt.Errorf("failed to allocate key buffer: %w", err)
	}

	// Copy each key to the buffer
	for i, key := range keys {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			buffer.Free()
			return nil, fmt.Errorf("invalid base64 key at index %d: %w", i, err)
		}
		if len(decoded) != 32 {
			buffer.Free()
			return nil, fmt.Errorf("key at index %d must be 32 bytes, got %d", i, len(decoded))
		}

		if success := cv.memory.Write(buffer.ptr+uint32(i*32), decoded); !success {
			buffer.Free()
			return nil, fmt.Errorf("failed to write key %d to WASM memory: %w", i, err)
		}
	}

	return buffer, nil
}

// readSizedBuffer reads a buffer with a 4-byte size prefix from WASM memory
func (cv *CryptoVote) readSizedBuffer(ptr uint32) ([]byte, error) {
	if ptr == 0 {
		return nil, fmt.Errorf("null pointer")
	}

	// Read the size (first 4 bytes)
	sizeBytes, ok := cv.memory.Read(ptr, 4)
	if !ok {
		return nil, fmt.Errorf("failed to read size from WASM memory")
	}

	size := binary.LittleEndian.Uint32(sizeBytes)
	if size > 10*1024*1024 { // 10MB safety limit
		return nil, fmt.Errorf("buffer size too large: %d bytes", size)
	}

	// Read the actual data
	data, ok := cv.memory.Read(ptr+4, size)
	if !ok {
		return nil, fmt.Errorf("failed to read data from WASM memory")
	}

	// Free the WASM memory (the WASM function expects us to do this)
	if err := cv.free(ptr, size+4); err != nil {
		log.Printf("Warning: failed to free WASM memory: %v", err)
	}

	return data, nil
}

// GenMixnetKeyPair generates a mixnet key pair
func (cv *CryptoVote) GenMixnetKeyPair() (*KeyPair, error) {
	results, err := cv.genMixnetKeyPair.Call(cv.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to call gen_mixnet_key_pair: %w", err)
	}

	ptr := uint32(results[0])
	if ptr == 0 {
		return nil, fmt.Errorf("key generation failed")
	}

	// Read the 64 bytes (32 bytes secret key + 32 bytes public key)
	data, ok := cv.memory.Read(ptr, 64)
	if !ok {
		return nil, fmt.Errorf("failed to read key pair from WASM memory")
	}

	// Free the WASM memory
	if err := cv.free(ptr, 64); err != nil {
		log.Printf("Warning: failed to free key pair memory: %v", err)
	}

	return &KeyPair{
		SecretKey: base64.StdEncoding.EncodeToString(data[:32]),
		PublicKey: base64.StdEncoding.EncodeToString(data[32:64]),
	}, nil
}

// GenTrusteeKeyPair generates a trustee key pair
func (cv *CryptoVote) GenTrusteeKeyPair() (*KeyPair, error) {
	results, err := cv.genTrusteeKeyPair.Call(cv.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to call gen_trustee_key_pair: %w", err)
	}

	ptr := uint32(results[0])
	if ptr == 0 {
		return nil, fmt.Errorf("key generation failed")
	}

	// Read the 64 bytes (32 bytes secret key + 32 bytes public key)
	data, ok := cv.memory.Read(ptr, 64)
	if !ok {
		return nil, fmt.Errorf("failed to read key pair from WASM memory")
	}

	// Free the WASM memory
	if err := cv.free(ptr, 64); err != nil {
		log.Printf("Warning: failed to free key pair memory: %v", err)
	}

	return &KeyPair{
		SecretKey: base64.StdEncoding.EncodeToString(data[:32]),
		PublicKey: base64.StdEncoding.EncodeToString(data[32:64]),
	}, nil
}

// EncryptResult represents the result of an encryption operation
type EncryptResult struct {
	Cyphers     [2][]byte `json:"cyphers"`
	ControlData []byte    `json:"control_data"`
}

// Encrypt encrypts a message using the mixnet and trustee public keys
func (cv *CryptoVote) Encrypt(mixnetPublicKeys, trusteePublicKeys []string, message string, maxSize uint32) (*EncryptResult, error) {
	// Input validation
	if err := validateKeyList(mixnetPublicKeys, "mixnet public keys"); err != nil {
		return nil, err
	}
	if err := validateKeyList(trusteePublicKeys, "trustee public keys"); err != nil {
		return nil, err
	}
	if message == "" {
		return nil, fmt.Errorf("message cannot be empty")
	}
	if maxSize == 0 || maxSize > 1024*1024 {
		return nil, fmt.Errorf("invalid max size: %d", maxSize)
	}
	if uint32(len(message)) > maxSize {
		return nil, fmt.Errorf("message too large: %d > %d", len(message), maxSize)
	}

	// Prepare mixnet keys
	mixnetBuffer, err := cv.copyKeysToWasm(mixnetPublicKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare mixnet keys: %w", err)
	}
	defer mixnetBuffer.Free()

	// Prepare trustee keys
	trusteeBuffer, err := cv.copyKeysToWasm(trusteePublicKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare trustee keys: %w", err)
	}
	defer trusteeBuffer.Free()

	// Prepare message
	messageBytes := []byte(message)
	messageBuffer, err := cv.NewWasmBuffer(uint32(len(messageBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to allocate message buffer: %w", err)
	}
	defer messageBuffer.Free()

	if err := messageBuffer.Write(messageBytes); err != nil {
		return nil, fmt.Errorf("failed to write message to WASM memory: %w", err)
	}

	// Call WASM encrypt function
	results, err := cv.encryptFunc.Call(cv.ctx,
		uint64(len(mixnetPublicKeys)),
		uint64(len(trusteePublicKeys)),
		uint64(mixnetBuffer.Ptr()),
		uint64(trusteeBuffer.Ptr()),
		uint64(messageBuffer.Ptr()),
		uint64(messageBuffer.Size()),
		uint64(maxSize),
	)
	if err != nil {
		return nil, fmt.Errorf("encrypt function call failed: %w", err)
	}

	resultPtr := uint32(results[0])
	if resultPtr == 0 {
		return nil, fmt.Errorf("encryption failed")
	}

	// Read the result
	encryptedData, err := cv.readSizedBuffer(resultPtr)
	if err != nil {
		return nil, fmt.Errorf("failed to read encrypted data: %w", err)
	}

	// Get cypher size to split the result
	cypherSizeResults, err := cv.cypherSizeFunc.Call(cv.ctx, uint64(len(mixnetPublicKeys)), uint64(maxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to get cypher size: %w", err)
	}
	cypherSize := uint32(cypherSizeResults[0])
	if cypherSize == 0 {
		return nil, fmt.Errorf("invalid cypher size")
	}

	if len(encryptedData) < int(cypherSize*2) {
		return nil, fmt.Errorf("encrypted data too small: %d < %d", len(encryptedData), cypherSize*2)
	}

	// Split the data into cyphers and control data
	cypher1 := make([]byte, cypherSize)
	cypher2 := make([]byte, cypherSize)
	copy(cypher1, encryptedData[:cypherSize])
	copy(cypher2, encryptedData[cypherSize:cypherSize*2])

	controlData := make([]byte, len(encryptedData)-int(cypherSize*2))
	copy(controlData, encryptedData[cypherSize*2:])

	return &EncryptResult{
		Cyphers:     [2][]byte{cypher1, cypher2},
		ControlData: controlData,
	}, nil
}

// DecryptMixnet decrypts a cypher block using a mixnet secret key
func (cv *CryptoVote) DecryptMixnet(secretKey string, cypherBlock []byte, cypherCount uint32) ([]byte, error) {
	// Input validation
	if secretKey == "" {
		return nil, fmt.Errorf("secret key cannot be empty")
	}
	if len(cypherBlock) == 0 {
		return nil, fmt.Errorf("cypher block cannot be empty")
	}
	if cypherCount == 0 {
		return nil, fmt.Errorf("cypher count cannot be zero")
	}
	if len(cypherBlock)%int(cypherCount) != 0 {
		return nil, fmt.Errorf("cypher block size not divisible by cypher count")
	}

	// Decode and validate secret key
	keyBytes, err := base64.StdEncoding.DecodeString(secretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid secret key base64: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(keyBytes))
	}

	// Allocate and copy the secret key
	keyBuffer, err := cv.NewWasmBuffer(32)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate key buffer: %w", err)
	}
	defer keyBuffer.Free()

	if err := keyBuffer.Write(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to write key to WASM memory: %w", err)
	}

	// Copy the cypher block
	cypherBuffer, err := cv.NewWasmBuffer(uint32(len(cypherBlock)))
	if err != nil {
		return nil, fmt.Errorf("failed to allocate cypher buffer: %w", err)
	}
	defer cypherBuffer.Free()

	if err := cypherBuffer.Write(cypherBlock); err != nil {
		return nil, fmt.Errorf("failed to write cypher block to WASM memory: %w", err)
	}

	// Call WASM decrypt_mixnet function
	results, err := cv.decryptMixnetFunc.Call(cv.ctx,
		uint64(keyBuffer.Ptr()),
		uint64(cypherCount),
		uint64(cypherBuffer.Ptr()),
		uint64(cypherBuffer.Size()),
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt_mixnet function call failed: %w", err)
	}

	resultPtr := uint32(results[0])
	if resultPtr == 0 {
		return nil, fmt.Errorf("mixnet decryption failed")
	}

	// Read the result
	return cv.readSizedBuffer(resultPtr)
}

// DecryptTrustee decrypts a cypher block using trustee secret keys
func (cv *CryptoVote) DecryptTrustee(secretKeys []string, cypherBlock []byte, cypherCount int) ([]string, error) {
	// Input validation
	if err := validateKeyList(secretKeys, "secret keys"); err != nil {
		return nil, err
	}
	if len(cypherBlock) == 0 {
		return nil, fmt.Errorf("cypher block cannot be empty")
	}
	if cypherCount == 0 {
		return nil, fmt.Errorf("cypher count cannot be zero")
	}
	if len(cypherBlock)%int(cypherCount) != 0 {
		return nil, fmt.Errorf("cypher block size not divisible by cypher count")
	}

	// Prepare trustee secret keys
	keysBuffer, err := cv.copyKeysToWasm(secretKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare secret keys: %w", err)
	}
	defer keysBuffer.Free()

	// Copy the cypher block
	cypherBuffer, err := cv.NewWasmBuffer(uint32(len(cypherBlock)))
	if err != nil {
		return nil, fmt.Errorf("failed to allocate cypher buffer: %w", err)
	}
	defer cypherBuffer.Free()

	if err := cypherBuffer.Write(cypherBlock); err != nil {
		return nil, fmt.Errorf("failed to write cypher block to WASM memory: %w", err)
	}

	// Call WASM decrypt_trustee function
	results, err := cv.decryptTrusteeFunc.Call(cv.ctx,
		uint64(len(secretKeys)),
		uint64(keysBuffer.Ptr()),
		uint64(cypherCount),
		uint64(cypherBuffer.Ptr()),
		uint64(cypherBuffer.Size()),
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt_trustee function call failed: %w", err)
	}

	resultPtr := uint32(results[0])
	if resultPtr == 0 {
		return nil, fmt.Errorf("trustee decryption failed")
	}

	// Read the result
	decryptedData, err := cv.readSizedBuffer(resultPtr)
	if err != nil {
		return nil, fmt.Errorf("failed to read decrypted data: %w", err)
	}

	// Parse decrypted data into individual messages
	if cypherCount == 1 {
		return []string{truncateAtNull(string(decryptedData))}, nil
	}

	if len(decryptedData)%int(cypherCount) != 0 {
		return nil, fmt.Errorf("decrypted data size not divisible by cypher count")
	}

	messageSize := len(decryptedData) / int(cypherCount)
	messages := make([]string, cypherCount)

	for i := range cypherCount {
		start := int(i) * messageSize
		end := start + messageSize
		messageBytes := decryptedData[start:end]
		messages[i] = truncateAtNull(string(messageBytes))
	}

	return messages, nil
}

// truncateAtNull truncates a string at the first null byte
func truncateAtNull(s string) string {
	if idx := bytes.IndexByte([]byte(s), 0); idx != -1 {
		return s[:idx]
	}
	return s
}

// Validate validates the cryptographic integrity of the voting process
func (cv *CryptoVote) Validate(encryptResults []*EncryptResult, mixnetDataList [][]byte, mixnetPublicKeys, trusteePublicKeys, trusteeSecretKeys []string, maxSize, userCount uint32) (int32, error) {
	// Input validation
	if len(encryptResults) == 0 {
		return -1000, fmt.Errorf("encrypt results cannot be empty")
	}
	if len(mixnetDataList) == 0 {
		return -1000, fmt.Errorf("mixnet data list cannot be empty")
	}
	if err := validateKeyList(mixnetPublicKeys, "mixnet public keys"); err != nil {
		return -1000, err
	}
	if err := validateKeyList(trusteePublicKeys, "trustee public keys"); err != nil {
		return -1000, err
	}
	if err := validateKeyList(trusteeSecretKeys, "trustee secret keys"); err != nil {
		return -1000, err
	}
	if maxSize == 0 || maxSize > 1024*1024 {
		return -1000, fmt.Errorf("invalid max size: %d", maxSize)
	}
	if userCount == 0 {
		return -1000, fmt.Errorf("user count cannot be zero")
	}
	if len(encryptResults) != int(userCount) {
		return -1000, fmt.Errorf("encrypt results length must match user count")
	}

	// Convert encryptResults to userDataBlock
	var totalSize int
	for _, result := range encryptResults {
		if len(result.Cyphers) != 2 {
			return -1000, fmt.Errorf("each encrypt result must have exactly 2 cyphers")
		}
		totalSize += len(result.Cyphers[0]) + len(result.Cyphers[1]) + len(result.ControlData)
	}

	userDataBlock := make([]byte, totalSize)
	offset := 0
	for _, result := range encryptResults {
		copy(userDataBlock[offset:], result.Cyphers[0])
		offset += len(result.Cyphers[0])
		copy(userDataBlock[offset:], result.Cyphers[1])
		offset += len(result.Cyphers[1])
		copy(userDataBlock[offset:], result.ControlData)
		offset += len(result.ControlData)
	}

	// Copy user data block to WASM
	userDataBuffer, err := cv.NewWasmBuffer(uint32(len(userDataBlock)))
	if err != nil {
		return -1000, fmt.Errorf("failed to allocate user data buffer: %w", err)
	}
	defer userDataBuffer.Free()

	if err := userDataBuffer.Write(userDataBlock); err != nil {
		return -1000, fmt.Errorf("failed to write user data to WASM memory: %w", err)
	}

	// Prepare mixnet size list
	mixnetSizeList := make([]byte, len(mixnetDataList)*4)
	for i, data := range mixnetDataList {
		binary.LittleEndian.PutUint32(mixnetSizeList[i*4:], uint32(len(data)))
	}

	mixnetSizeBuffer, err := cv.NewWasmBuffer(uint32(len(mixnetSizeList)))
	if err != nil {
		return -1000, fmt.Errorf("failed to allocate mixnet size buffer: %w", err)
	}
	defer mixnetSizeBuffer.Free()

	if err := mixnetSizeBuffer.Write(mixnetSizeList); err != nil {
		return -1000, fmt.Errorf("failed to write mixnet size list to WASM memory: %w", err)
	}

	// Concatenate mixnet data blocks
	var totalMixnetSize int
	for _, data := range mixnetDataList {
		totalMixnetSize += len(data)
	}
	mixnetDataBlock := make([]byte, totalMixnetSize)
	offset = 0
	for _, data := range mixnetDataList {
		copy(mixnetDataBlock[offset:], data)
		offset += len(data)
	}

	mixnetDataBuffer, err := cv.NewWasmBuffer(uint32(len(mixnetDataBlock)))
	if err != nil {
		return -1000, fmt.Errorf("failed to allocate mixnet data buffer: %w", err)
	}
	defer mixnetDataBuffer.Free()

	if err := mixnetDataBuffer.Write(mixnetDataBlock); err != nil {
		return -1000, fmt.Errorf("failed to write mixnet data to WASM memory: %w", err)
	}

	// Prepare mixnet public keys
	mixnetKeysBuffer, err := cv.copyKeysToWasm(mixnetPublicKeys)
	if err != nil {
		return -1000, fmt.Errorf("failed to prepare mixnet public keys: %w", err)
	}
	defer mixnetKeysBuffer.Free()

	// Prepare trustee public keys
	trusteePublicKeysBuffer, err := cv.copyKeysToWasm(trusteePublicKeys)
	if err != nil {
		return -1000, fmt.Errorf("failed to prepare trustee public keys: %w", err)
	}
	defer trusteePublicKeysBuffer.Free()

	// Prepare trustee secret keys
	trusteeSecretKeysBuffer, err := cv.copyKeysToWasm(trusteeSecretKeys)
	if err != nil {
		return -1000, fmt.Errorf("failed to prepare trustee secret keys: %w", err)
	}
	defer trusteeSecretKeysBuffer.Free()

	// Call WASM validate function
	results, err := cv.validateFunc.Call(cv.ctx,
		uint64(userCount),
		uint64(len(trusteeSecretKeys)),
		uint64(userDataBuffer.Ptr()),
		uint64(userDataBuffer.Size()),
		uint64(maxSize),
		uint64(mixnetSizeBuffer.Ptr()),
		uint64(mixnetSizeBuffer.Size()),
		uint64(mixnetDataBuffer.Ptr()),
		uint64(mixnetDataBuffer.Size()),
		uint64(mixnetKeysBuffer.Ptr()),
		uint64(trusteePublicKeysBuffer.Ptr()),
		uint64(trusteeSecretKeysBuffer.Ptr()),
	)
	if err != nil {
		return -1000, fmt.Errorf("validate function call failed: %w", err)
	}

	result := int32(results[0])
	return result, nil
}
