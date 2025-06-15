async function loadCryptoVote(wasmFile) {
  let instance;
  let callbacks = {};

  const importObject = {
    env: {
      console_log: (ptr, len) => {
        const memory = instance.exports.memory;
        const bytes = new Uint8Array(memory.buffer, ptr, len);
        const message = new TextDecoder().decode(bytes);
        console.log(`Wasm CryptoVote Module: ${message}`);
      },
      get_random: (ptr, amount) => {
        const memory = instance.exports.memory;
        const buffer = new Uint8Array(memory.buffer, ptr, amount);
        crypto.getRandomValues(buffer);
      },
      publish_key_public: (keyPtr) => {
        try {
          const memory = instance.exports.memory;
          const keySize = 32; // Assuming 32 bytes for public key
          const keyBytes = new Uint8Array(memory.buffer, keyPtr, keySize);

          // Convert to base64 for transport
          const keyBase64 = btoa(String.fromCharCode(...keyBytes));

          if (callbacks.publishKeyPublic) {
            return callbacks.publishKeyPublic(keyBase64);
          }

          // Default implementation - send to server
          return sendPublishKeyPublic(keyBase64);
        } catch (error) {
          console.error("Error in publish_key_public:", error);
          return 1; // Error
        }
      },
    },
  };

  try {
    const result = await WebAssembly.instantiateStreaming(
      fetch(wasmFile),
      importObject,
    );
    instance = result.instance;
  } catch (error) {
    console.error("Failed to load WebAssembly module:", error);
    throw new Error(
      "Failed to initialize WebAssembly module: " + error.message,
    );
  }

  // Get exports
  const {
    alloc,
    free,
    start: wasmStart,
    onmessage: wasmOnMessage,
    memory,
  } = instance.exports;

  // Helper function to copy from WASM memory to JavaScript
  function copyFromWasm(ptr, size) {
    if (!ptr || size <= 0) {
      throw new Error("Invalid pointer or size");
    }
    const result = new Uint8Array(size);
    const memoryView = new Uint8Array(memory.buffer, ptr, size);
    result.set(memoryView);
    return result;
  }

  // Helper function to allocate memory in WASM and copy data from JavaScript
  function copyToWasm(data) {
    if (!data || data.length === 0) {
      throw new Error("Data cannot be null or empty");
    }

    const dataLength = data.length;
    const dataPtr = alloc(dataLength);

    if (!dataPtr) {
      throw new Error("Failed to allocate memory in WebAssembly");
    }

    try {
      const memoryView = new Uint8Array(memory.buffer, dataPtr, dataLength);
      memoryView.set(data);
      return { ptr: dataPtr, size: dataLength };
    } catch (error) {
      // Clean up on error
      free(dataPtr, dataLength);
      throw error;
    }
  }

  // Convert string to Uint8Array
  function stringToUint8Array(str) {
    return new TextEncoder().encode(str);
  }

  // Convert Uint8Array to string
  function uint8ArrayToString(array) {
    return new TextDecoder().decode(array);
  }

  // Default implementation for publishKeyPublic
  async function sendPublishKeyPublic(keyBase64) {
    try {
      const response = await fetch("/publish_key_public", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${getCurrentUserId()}`,
        },
        body: JSON.stringify(keyBase64),
      });

      if (response.ok) {
        return 0; // Success
      } else {
        console.error("Failed to publish public key:", response.status);
        return 1; // Error
      }
    } catch (error) {
      console.error("Error publishing public key:", error);
      return 1; // Error
    }
  }

  // Get current user ID from global state or DOM
  function getCurrentUserId() {
    // This should be set by the client application
    if (window.currentUserId) {
      return window.currentUserId;
    }
    const userIdElement = document.getElementById("userId");
    if (userIdElement && userIdElement.value) {
      return parseInt(userIdElement.value);
    }
    return null;
  }

  // Get vote message from DOM
  function getVoteMessage() {
    const voteInput = document.getElementById("voteMessage");
    return voteInput ? voteInput.value : "";
  }

  // Public API
  const api = {
    // Initialize the WASM app with user ID
    start: function (userId) {
      if (!userId || userId < 1) {
        throw new Error("Invalid user ID");
      }

      // Store user ID globally for callback access
      window.currentUserId = userId;

      try {
        wasmStart(userId);
        console.log(`CryptoVote WASM app started with user ID: ${userId}`);
      } catch (error) {
        console.error("Failed to start WASM app:", error);
        throw error;
      }
    },

    // Handle incoming messages from EventSource
    onmessage: function (eventData) {
      if (!wasmOnMessage) {
        console.warn("WASM onmessage function not available");
        return;
      }

      try {
        let messageBytes;

        if (typeof eventData === "string") {
          messageBytes = stringToUint8Array(eventData);
        } else if (eventData instanceof Uint8Array) {
          messageBytes = eventData;
        } else {
          // Assume it's JSON and stringify it
          messageBytes = stringToUint8Array(JSON.stringify(eventData));
        }

        const wasmData = copyToWasm(messageBytes);

        try {
          wasmOnMessage(wasmData.ptr, wasmData.size);
        } finally {
          // Always free the allocated memory
          free(wasmData.ptr, wasmData.size);
        }
      } catch (error) {
        console.error("Error processing message in WASM:", error);
        // Don't re-throw, just log the error
      }
    },

    // Set custom callback functions
    setCallbacks: function (newCallbacks) {
      callbacks = { ...callbacks, ...newCallbacks };
    },

    // Get vote message (for WASM to access)
    getVoteMessage: getVoteMessage,

    // Memory management utilities
    alloc: function (size) {
      return alloc(size);
    },

    free: function (ptr, size) {
      return free(ptr, size);
    },

    // Copy utilities
    copyToWasm: copyToWasm,
    copyFromWasm: copyFromWasm,

    // String conversion utilities
    stringToUint8Array: stringToUint8Array,
    uint8ArrayToString: uint8ArrayToString,

    // Access to raw WASM instance (for advanced usage)
    getInstance: function () {
      return instance;
    },

    // Check if WASM is loaded and ready
    isReady: function () {
      return (
        instance !== null &&
        instance !== undefined &&
        wasmStart &&
        wasmOnMessage
      );
    },

    // Get WASM memory for debugging
    getMemory: function () {
      return instance ? instance.exports.memory : null;
    },
  };

  // Make some utilities globally available for WASM callbacks
  window.cryptoVoteGetVoteMessage = getVoteMessage;
  window.cryptoVoteGetCurrentUserId = getCurrentUserId;

  return api;
}

// Export for use in other scripts
if (typeof module !== "undefined" && module.exports) {
  module.exports = { loadCryptoVote };
} else if (typeof window !== "undefined") {
  window.loadCryptoVote = loadCryptoVote;
}
