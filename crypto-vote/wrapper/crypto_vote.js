async function loadCryptoVote(wasmFile, callbacks) {
  let instance;

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
          const keyAsString = String.fromCharCode(...keyBytes);

          if (callbacks.publishKeyPublic) {
            return callbacks.publishKeyPublic(keyAsString);
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
    memory,
    alloc,
    free,
    start: wasmStart,
    onmessage: wasmOnMessage,
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

  // TODO: Move this code to the client.html
  // // Default implementation for publishKeyPublic
  // async function sendPublishKeyPublic(keyBase64) {
  //   try {
  //     const response = await fetch("/publish_key_public", {
  //       method: "POST",
  //       headers: {
  //         "Content-Type": "application/json",
  //         Authorization: `Bearer ${getCurrentUserId()}`,
  //       },
  //       body: JSON.stringify(keyBase64),
  //     });

  //     if (response.ok) {
  //       return 0; // Success
  //     } else {
  //       console.error("Failed to publish public key:", response.status);
  //       return 1; // Error
  //     }
  //   } catch (error) {
  //     console.error("Error publishing public key:", error);
  //     return 1; // Error
  //   }
  // }

  // Public API
  const api = {
    // Initialize the WASM app with user ID
    start: function (userId) {
      if (!userId || userId < 1) {
        throw new Error("Invalid user ID");
      }

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
      try {
        if (typeof eventData !== "string") {
          throw new Error("Invalid message format");
        }

        const messageBytes = stringToUint8Array(eventData);
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
  };

  return api;
}
