// WebWorker for WASM Crypto Vote Module
// This worker handles the WASM module execution to prevent blocking the main thread

let wasmInstance = null;
let isInitialized = false;

// Message types for communication with main thread
const MESSAGE_TYPES = {
  INIT: 'init',
  START: 'start',
  PROCESS_EVENT: 'process_event',
  ENCRYPT_AND_SEND_VOTE: 'encrypt_and_send_vote',
  RESPONSE: 'response',
  ERROR: 'error',
  CALLBACK: 'callback'
};

// Callback types that need to be handled by main thread
const CALLBACK_TYPES = {
  PUBLISH_KEY_PUBLIC: 'publish_key_public',
  PUBLISH_VOTE: 'publish_vote',
  SET_CAN_VOTE: 'set_can_vote',
  LOG: 'log'
};

// Helper functions for memory management
function copyToWasm(data) {
  if (!data || data.length === 0) {
    throw new Error('Data cannot be null or empty');
  }

  const dataLength = data.length;
  const dataPtr = wasmInstance.exports.alloc(dataLength);

  if (!dataPtr) {
    throw new Error('Failed to allocate memory in WebAssembly');
  }

  try {
    const memoryView = new Uint8Array(wasmInstance.exports.memory.buffer, dataPtr, dataLength);
    memoryView.set(data);
    return { ptr: dataPtr, size: dataLength };
  } catch (error) {
    wasmInstance.exports.free(dataPtr, dataLength);
    throw error;
  }
}

function copyFromWasm(ptr, size) {
  if (!ptr || size <= 0) {
    throw new Error('Invalid pointer or size');
  }
  const result = new Uint8Array(size);
  const memoryView = new Uint8Array(wasmInstance.exports.memory.buffer, ptr, size);
  result.set(memoryView);
  return result;
}

function stringToUint8Array(str) {
  return new TextEncoder().encode(str);
}

function uint8ArrayToString(array) {
  return new TextDecoder().decode(array);
}

// Send callback request to main thread and wait for response
async function sendCallback(type, data) {
  return new Promise((resolve, reject) => {
    const callbackId = Date.now() + Math.random();

    const timeout = setTimeout(() => {
      reject(new Error(`Callback timeout: ${type}`));
    }, 30000); // 30 second timeout

    const handler = (event) => {
      if (event.data.type === MESSAGE_TYPES.CALLBACK &&
          event.data.callbackId === callbackId) {
        clearTimeout(timeout);
        self.removeEventListener('message', handler);

        if (event.data.error) {
          reject(new Error(event.data.error));
        } else {
          resolve(event.data.result);
        }
      }
    };

    self.addEventListener('message', handler);

    self.postMessage({
      type: MESSAGE_TYPES.CALLBACK,
      callbackType: type,
      callbackId: callbackId,
      data: data
    });
  });
}

// WASM import object with callback implementations
function createImportObject() {
  return {
    env: {
      console_log: (ptr, len) => {
        try {
          const bytes = new Uint8Array(wasmInstance.exports.memory.buffer, ptr, len);
          const message = uint8ArrayToString(bytes);

          // Send log message to main thread (fire and forget)
          self.postMessage({
            type: MESSAGE_TYPES.CALLBACK,
            callbackType: CALLBACK_TYPES.LOG,
            data: { message }
          });
        } catch (error) {
          console.error('Error in console_log callback:', error);
        }
      },

      get_random: (ptr, amount) => {
        try {
          const buffer = new Uint8Array(wasmInstance.exports.memory.buffer, ptr, amount);
          crypto.getRandomValues(buffer);
        } catch (error) {
          console.error('Error in get_random callback:', error);
          // Fill with zeros as fallback (not cryptographically secure!)
          const buffer = new Uint8Array(wasmInstance.exports.memory.buffer, ptr, amount);
          buffer.fill(0);
        }
      },

      publish_key_public: (keyPtr) => {
        try {
          const keySize = 32; // Based on CallbackInterface.zig publicKeySize calculation
          const keyBytes = new Uint8Array(wasmInstance.exports.memory.buffer, keyPtr, keySize);

          // Convert to base64 for transmission
          const keyArray = Array.from(keyBytes);
          const keyBase64 = btoa(String.fromCharCode(...keyArray));

          // Send callback to main thread - this is synchronous from WASM perspective
          // but we need to handle it asynchronously
          sendCallback(CALLBACK_TYPES.PUBLISH_KEY_PUBLIC, { key: keyBase64 })
            .then(result => {
              // Result handling is done in the promise resolution
            })
            .catch(error => {
              console.error('Error in publish_key_public callback:', error);
            });

          return 0; // Return success immediately, actual result handled async
        } catch (error) {
          console.error('Error in publish_key_public callback:', error);
          return 1; // Error
        }
      },

      publish_vote: (votePtr, voteLen) => {
        try {
          const voteBytes = new Uint8Array(wasmInstance.exports.memory.buffer, votePtr, voteLen);
          const voteData = Array.from(voteBytes);

          sendCallback(CALLBACK_TYPES.PUBLISH_VOTE, { vote: voteData })
            .then(result => {
              // Result handling is done in the promise resolution
            })
            .catch(error => {
              console.error('Error in publish_vote callback:', error);
            });

          return 0; // Return success immediately
        } catch (error) {
          console.error('Error in publish_vote callback:', error);
          return 1; // Error
        }
      },

      set_can_vote: (size) => {
        try {
          const canVote = size !== 0;

          // Send callback to main thread (fire and forget)
          self.postMessage({
            type: MESSAGE_TYPES.CALLBACK,
            callbackType: CALLBACK_TYPES.SET_CAN_VOTE,
            data: { canVote, size }
          });
        } catch (error) {
          console.error('Error in set_can_vote callback:', error);
        }
      }
    }
  };
}

// Initialize WASM module
async function initializeWasm(wasmArrayBuffer) {
  try {
    const importObject = createImportObject();
    const result = await WebAssembly.instantiate(wasmArrayBuffer, importObject);
    wasmInstance = result.instance;
    isInitialized = true;

    return {
      success: true,
      message: 'WASM module initialized successfully'
    };
  } catch (error) {
    return {
      success: false,
      error: `Failed to initialize WASM module: ${error.message}`
    };
  }
}

// Start the WASM application
function startApp(userId) {
  if (!isInitialized || !wasmInstance) {
    throw new Error('WASM module not initialized');
  }

  if (!userId || userId < 1) {
    throw new Error('Invalid user ID');
  }

  try {
    wasmInstance.exports.start(userId);
    return {
      success: true,
      message: `WASM app started with user ID: ${userId}`
    };
  } catch (error) {
    throw new Error(`Failed to start WASM app: ${error.message}`);
  }
}

// Process incoming event
function processEvent(eventData) {
  if (!isInitialized || !wasmInstance) {
    throw new Error('WASM module not initialized');
  }

  if (typeof eventData !== 'string') {
    throw new Error('Event data must be a string');
  }

  const messageBytes = stringToUint8Array(eventData);
  const wasmData = copyToWasm(messageBytes);

  try {
    wasmInstance.exports.onmessage(wasmData.ptr, wasmData.size);
    return {
      success: true,
      message: 'Event processed successfully'
    };
  } catch (error) {
    throw new Error(`Failed to process event: ${error.message}`);
  } finally {
    wasmInstance.exports.free(wasmData.ptr, wasmData.size);
  }
}

// Encrypt and send vote
function encryptAndSendVote(voteData) {
  if (!isInitialized || !wasmInstance) {
    throw new Error('WASM module not initialized');
  }

  if (typeof voteData !== 'string') {
    throw new Error('Vote data must be a string');
  }

  const voteBytes = stringToUint8Array(voteData);
  const wasmData = copyToWasm(voteBytes);

  try {
    const result = wasmInstance.exports.encrypt_and_send_vote(wasmData.ptr, wasmData.size);

    if (result === 0) {
      return {
        success: true,
        message: 'Vote encrypted and sent successfully'
      };
    } else {
      throw new Error(`Vote encryption failed with code: ${result}`);
    }
  } catch (error) {
    throw new Error(`Failed to encrypt and send vote: ${error.message}`);
  } finally {
    wasmInstance.exports.free(wasmData.ptr, wasmData.size);
  }
}

// Message handler for communication with main thread
self.addEventListener('message', async (event) => {
  const { type, id, data } = event.data;

  try {
    let result;

    switch (type) {
      case MESSAGE_TYPES.INIT:
        result = await initializeWasm(data.wasmArrayBuffer);
        break;

      case MESSAGE_TYPES.START:
        result = startApp(data.userId);
        break;

      case MESSAGE_TYPES.PROCESS_EVENT:
        result = processEvent(data.eventData);
        break;

      case MESSAGE_TYPES.ENCRYPT_AND_SEND_VOTE:
        result = encryptAndSendVote(data.voteData);
        break;

      default:
        throw new Error(`Unknown message type: ${type}`);
    }

    // Send success response
    self.postMessage({
      type: MESSAGE_TYPES.RESPONSE,
      id: id,
      success: true,
      data: result
    });

  } catch (error) {
    // Send error response
    self.postMessage({
      type: MESSAGE_TYPES.ERROR,
      id: id,
      success: false,
      error: error.message
    });
  }
});

// Handle uncaught errors
self.addEventListener('error', (event) => {
  self.postMessage({
    type: MESSAGE_TYPES.ERROR,
    error: `Worker error: ${event.message}`,
    filename: event.filename,
    lineno: event.lineno
  });
});

// Handle unhandled promise rejections
self.addEventListener('unhandledrejection', (event) => {
  self.postMessage({
    type: MESSAGE_TYPES.ERROR,
    error: `Unhandled promise rejection: ${event.reason}`
  });
});
