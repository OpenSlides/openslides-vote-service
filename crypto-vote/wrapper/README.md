# CryptoVote WebWorker Wrapper

This wrapper provides a clean interface to the WASM crypto vote module running in a WebWorker to prevent blocking the main thread during cryptographic operations.

## Overview

The wrapper consists of two main components:

1. **CryptoVoteWrapper** (`crypto_vote_wrapper.js`) - Main wrapper class that manages communication with the WebWorker
2. **WebWorker** (`crypto_vote_worker.js`) - WebWorker that loads and executes the WASM module

## Features

- **Non-blocking execution**: All WASM operations run in a WebWorker to prevent UI freezing
- **Clean API**: Simple JavaScript interface with proper error handling
- **Memory safety**: No direct access to WASM memory from outside the wrapper
- **Callback system**: Proper handling of WASM callbacks with async support
- **Type safety**: Input validation and proper error messages

## Usage

### Basic Example

```javascript
import CryptoVoteWrapper from './crypto_vote_wrapper.js';

// Create wrapper instance
const cryptoVote = new CryptoVoteWrapper();

// Initialize with WASM file and callbacks
await cryptoVote.init('./crypto-vote.wasm', {
  onPublishKeyPublic: async (keyBase64) => {
    // Handle public key publication
    const response = await fetch('/api/publish-key', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: keyBase64 })
    });
    return response.ok ? { success: true } : { success: false };
  },

  onPublishVote: async (voteData) => {
    // Handle vote publication
    const response = await fetch('/api/publish-vote', {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: new Uint8Array(voteData)
    });
    return response.ok ? { success: true } : { success: false };
  },

  onSetCanVote: (canVote, size) => {
    // Handle voting status changes
    console.log(`Voting status: ${canVote ? 'enabled' : 'disabled'}`);
  },

  onLog: (message) => {
    // Handle log messages from WASM
    console.log(`WASM: ${message}`);
  }
});

// Start the application
await cryptoVote.start(123); // user ID

// Process events
await cryptoVote.processEvent('{"type": "key_exchange", "data": "..."}');

// Send a vote
await cryptoVote.encryptAndSendVote('{"candidate": "Alice", "ballot": "ballot_123"}');

// Cleanup when done
cryptoVote.destroy();
```

### Browser Global Usage

If not using ES modules, the wrapper is available as a global class:

```html
<script src="crypto_vote_wrapper.js"></script>
<script>
  const cryptoVote = new CryptoVoteWrapper();
  // ... use as above
</script>
```

### Node.js Usage

For Node.js environments:

```javascript
const CryptoVoteWrapper = require('./crypto_vote_wrapper.js');
const cryptoVote = new CryptoVoteWrapper();
// ... use as above
```

## API Reference

### CryptoVoteWrapper

#### constructor()

Creates a new wrapper instance.

#### async init(wasmUrl, callbacks)

Initializes the WASM module in a WebWorker.

**Parameters:**
- `wasmUrl` (string): URL or path to the WASM file
- `callbacks` (object): Callback functions (see below)

**Callbacks:**
- `onPublishKeyPublic(keyBase64)`: Called when public key needs to be published
  - `keyBase64` (string): Base64 encoded public key
  - Returns: Promise that resolves to `{ success: boolean }`
- `onPublishVote(voteData)`: Called when vote needs to be published
  - `voteData` (number[]): Vote data as array of bytes
  - Returns: Promise that resolves to `{ success: boolean }`
- `onSetCanVote(canVote, size)`: Called when voting status changes
  - `canVote` (boolean): Whether voting is allowed
  - `size` (number): Size parameter from WASM
- `onLog(message)`: Called for log messages from WASM
  - `message` (string): Log message

#### async start(userId)

Starts the crypto vote application.

**Parameters:**
- `userId` (number): User ID (must be positive)

#### async processEvent(eventData)

Processes an incoming event.

**Parameters:**
- `eventData` (string): Event data as JSON string

#### async encryptAndSendVote(voteData)

Encrypts and sends a vote.

**Parameters:**
- `voteData` (string): Vote data as JSON string

#### destroy()

Cleans up resources and terminates the WebWorker.

## Error Handling

All async methods throw errors that should be caught:

```javascript
try {
  await cryptoVote.start(123);
} catch (error) {
  console.error('Failed to start:', error.message);
}
```

Common error scenarios:
- WASM file not found or invalid
- WebWorker initialization failure
- Invalid parameters (user ID, event data, etc.)
- Callback execution errors
- WASM runtime errors

## Security Considerations

1. **Memory Isolation**: The WASM module runs in a WebWorker with no access to the main thread's memory or DOM
2. **No Global Access**: The wrapper doesn't access `window`, `document`, or other global objects
3. **Input Validation**: All inputs are validated before being passed to WASM
4. **Callback Sandboxing**: Callbacks are executed with proper error handling to prevent crashes

## Performance Notes

- Initial loading includes WASM compilation overhead
- All operations are asynchronous and non-blocking
- Memory is managed automatically with proper cleanup
- WebWorker communication has small serialization overhead

## Browser Compatibility

- Modern browsers with WebWorker and WebAssembly support
- Requires ES6+ features (Promises, async/await)
- Uses Fetch API for WASM loading

## Migration from Old Wrapper

If you're migrating from the old wrapper (`crypto_vote.js`):

### Old API:
```javascript
const api = await loadCryptoVote('./crypto-vote.wasm', callbacks);
api.start(123);
api.onmessage(eventData);
```

### New API:
```javascript
const cryptoVote = new CryptoVoteWrapper();
await cryptoVote.init('./crypto-vote.wasm', callbacks);
await cryptoVote.start(123);
await cryptoVote.processEvent(eventData);
```

Key differences:
- Constructor pattern instead of factory function
- Explicit `init()` call required
- All operations are async and return Promises
- Added `encryptAndSendVote()` method
- Proper resource cleanup with `destroy()`

## Debugging

Enable detailed logging by providing an `onLog` callback:

```javascript
await cryptoVote.init('./crypto-vote.wasm', {
  onLog: (message) => {
    console.log(`[${new Date().toISOString()}] WASM: ${message}`);
  }
  // ... other callbacks
});
```

For WebWorker debugging, check browser developer tools' "Sources" or "Workers" tab.

## Example

See `example.html` for a complete working example with a web interface.