async function loadPerformanceTest(wasmFile) {
  const importObject = {
    env: {
      console_log: (ptr, len) => {
        const memory = instance.exports.memory;
        const bytes = new Uint8Array(memory.buffer, ptr, len);
        const message = new TextDecoder().decode(bytes);
        console.log(message);
      },
      get_random: (ptr, amount) => {
        const memory = instance.exports.memory;
        const buffer = new Uint8Array(memory.buffer, ptr, amount);
        crypto.getRandomValues(buffer);
      },
    },
  };

  let instance;
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
    gen_mixnet_key_pair: wasm_gen_mixnet_key_pair,
    gen_trustee_key_pair: wasm_gen_trustee_key_pair,
    encrypt: wasm_encrypt,
    decrypt_mixnet: wasm_decrypt_mixnet,
    decrypt_trustee: wasm_decrypt_trustee,
    memory,
  } = instance.exports;

  // Helper function to copy from WASM memory to JavaScript
  function copyFromWasm(ptr, size) {
    const result = new Uint8Array(size);
    const memoryView = new Uint8Array(memory.buffer, ptr, size);
    result.set(memoryView);
    return result;
  }

  // Helper function to allocate memory in WASM and copy data from JavaScript
  function copyToWasm(data) {
    const dataLength = data.length;
    const dataPtr = alloc(dataLength);

    if (!dataPtr) {
      throw new Error("Failed to allocate memory in WebAssembly");
    }

    const memoryView = new Uint8Array(memory.buffer, dataPtr, dataLength);
    memoryView.set(data);

    return { ptr: dataPtr, size: dataLength };
  }

  // Helper function to read a sized buffer (4 bytes size prefix)
  function readSizedBuffer(ptr) {
    if (!ptr) return null;

    try {
      // Read the size (first 4 bytes)
      const sizeView = new DataView(memory.buffer, ptr, 4);
      const size = sizeView.getUint32(0, true); // Little endian

      // Copy the actual data
      const result = copyFromWasm(ptr + 4, size);

      // Free the WASM memory
      free(ptr, size + 4);

      return result;
    } catch (error) {
      console.error("Error reading sized buffer:", error);
      // Make sure to free memory even if an error occurs
      try {
        free(ptr, 4); // Free at least the header if we couldn't read the size
      } catch (e) {
        console.error("Failed to free memory after error:", e);
      }
      throw new Error("Failed to read sized buffer: " + error.message);
    }
  }

  // Helper to copy an array of 32-byte keys to WASM memory
  function copyKeysToWasm(keyList) {
    if (!Array.isArray(keyList) || keyList.length === 0) {
      throw new Error("Key list must be a non-empty array");
    }

    // Allocate memory for all keys (32 bytes each)
    const totalSize = keyList.length * 32;
    const keysPtr = alloc(totalSize);

    if (!keysPtr) {
      throw new Error("Failed to allocate memory for keys");
    }

    try {
      // Copy each key to WASM memory
      const memoryView = new Uint8Array(memory.buffer);
      for (let i = 0; i < keyList.length; i++) {
        const key = keyList[i];
        if (!key || key.length !== 32) {
          throw new Error(
            `Key at index ${i} must be 32 bytes, got ${key ? key.length : "undefined"}`,
          );
        }
        memoryView.set(key, keysPtr + i * 32);
      }

      return { ptr: keysPtr, count: keyList.length };
    } catch (error) {
      // Clean up allocated memory if an error occurs
      free(keysPtr, totalSize);
      throw error;
    }
  }

  // String to Uint8Array conversion
  function stringToUint8Array(str) {
    return new TextEncoder().encode(str);
  }

  // Uint8Array to String conversion
  function uint8ArrayToString(array) {
    return new TextDecoder().decode(array);
  }

  return {
    gen_mixnet_key_pair: () => {
      try {
        const keypairPtr = wasm_gen_mixnet_key_pair();
        if (!keypairPtr) {
          throw new Error("Failed to generate mixnet key pair");
        }

        // Copy the 64 bytes (32 bytes secret key + 32 bytes public key)
        const result = copyFromWasm(keypairPtr, 64);

        // Free the WASM memory
        free(keypairPtr, 64);

        return {
          secretKey: result.slice(0, 32),
          publicKey: result.slice(32, 64),
        };
      } catch (error) {
        console.error("Error generating mixnet key pair:", error);
        throw new Error("Failed to generate mixnet key pair: " + error.message);
      }
    },

    gen_trustee_key_pair: () => {
      try {
        const keypairPtr = wasm_gen_trustee_key_pair();
        if (!keypairPtr) {
          throw new Error("Failed to generate trustee key pair");
        }

        // Copy the 64 bytes (32 bytes secret key + 32 bytes public key)
        const result = copyFromWasm(keypairPtr, 64);

        // Free the WASM memory
        free(keypairPtr, 64);

        return {
          secretKey: result.slice(0, 32),
          publicKey: result.slice(32, 64),
        };
      } catch (error) {
        console.error("Error generating trustee key pair:", error);
        throw new Error(
          "Failed to generate trustee key pair: " + error.message,
        );
      }
    },

    encrypt: (mixnetPublicKeyList, trusteePublicKeyList, message) => {
      try {
        if (!message || typeof message !== "string" || message.length === 0) {
          throw new Error("Message must be a non-empty string");
        }

        // Convert string message to Uint8Array
        const messageBytes = stringToUint8Array(message);

        // Prepare mixnet keys
        const mixnetKeys = copyKeysToWasm(mixnetPublicKeyList);

        // Prepare trustee keys
        const trusteeKeys = copyKeysToWasm(trusteePublicKeyList);

        // Prepare message
        const messageData = copyToWasm(messageBytes);

        // Call WASM encrypt function
        const resultPtr = wasm_encrypt(
          mixnetKeys.count,
          trusteeKeys.count,
          mixnetKeys.ptr,
          trusteeKeys.ptr,
          messageData.ptr,
          messageData.size,
        );

        // The WASM function deallocates the inputs

        if (!resultPtr) {
          throw new Error("Encryption failed");
        }

        // Read the result (sized buffer)
        return readSizedBuffer(resultPtr);
      } catch (error) {
        console.error("Error during encryption:", error);
        throw new Error("Encryption failed: " + error.message);
      }
    },

    decrypt_mixnet: (secretKey, cypherBlock, cypherCount) => {
      if (secretKey.length !== 32) {
        throw new Error("Secret key must be 32 bytes");
      }

      // Allocate and copy the secret key
      const keyPtr = alloc(32);
      if (!keyPtr) {
        throw new Error("Failed to allocate memory for secret key");
      }

      const keyView = new Uint8Array(memory.buffer, keyPtr, 32);
      keyView.set(secretKey);

      // Copy the cypher block
      const cypherData = copyToWasm(cypherBlock);

      // Call WASM decrypt_mixnet function
      const resultPtr = wasm_decrypt_mixnet(
        keyPtr,
        cypherCount,
        cypherData.ptr,
        cypherData.size,
      );

      // The WASM function deallocates the inputs

      if (!resultPtr) {
        throw new Error("Mixnet decryption failed");
      }

      // Read the result (sized buffer)
      return readSizedBuffer(resultPtr);
    },

    decrypt_trustee: (secretKeyList, cypherBlock, cypherCount) => {
      try {
        // Prepare trustee secret keys
        const trusteeKeys = copyKeysToWasm(secretKeyList);

        // Copy the cypher block
        const cypherData = copyToWasm(cypherBlock);

        // Call WASM decrypt_trustee function
        const resultPtr = wasm_decrypt_trustee(
          trusteeKeys.count,
          trusteeKeys.ptr,
          cypherCount,
          cypherData.ptr,
          cypherData.size,
        );

        if (!resultPtr) {
          throw new Error("Trustee decryption failed");
        }

        // Read the result (sized buffer) and convert to array of strings
        const decryptedData = readSizedBuffer(resultPtr);

        // Parse decrypted data into individual messages
        // Assuming each message is a UTF-8 encoded string and messages are
        // packed one after another with no delimiter

        // Since we don't know how the messages are delimited in the binary data,
        // we need to implement a method to separate them

        // If cypherCount is 1, we assume the entire buffer is a single message
        if (cypherCount === 1) {
          return [uint8ArrayToString(decryptedData)];
        }

        // For multiple messages, we need to determine the message boundaries
        // This implementation assumes equal-sized messages for simplicity
        // You might need to adjust this based on your actual data format
        const messageSize = decryptedData.length / cypherCount;
        const messages = [];

        for (let i = 0; i < cypherCount; i++) {
          const start = i * messageSize;
          const end = start + messageSize;
          const messageBytes = decryptedData.slice(start, end);
          messages.push(uint8ArrayToString(messageBytes));
        }

        return messages;
      } catch (error) {
        console.error("Error during trustee decryption:", error);
        throw new Error("Trustee decryption failed: " + error.message);
      }
    },
  };
}
