async function loadCryptoVote(wasmFile) {
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
    cypher_size: wasm_cypher_size,
    validate: wasm_validate,
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

  // Helper function to read a sized buffer (4 bytes size prefix)
  function readSizedBuffer(ptr) {
    if (!ptr) return null;

    let size = 0;
    try {
      // Read the size (first 4 bytes)
      const sizeView = new DataView(memory.buffer, ptr, 4);
      size = sizeView.getUint32(0, true); // Little endian

      if (size > 10 * 1024 * 1024) {
        // 10MB safety limit
        throw new Error(`Buffer size too large: ${size} bytes`);
      }

      // Copy the actual data
      const result = copyFromWasm(ptr + 4, size);

      // Free the WASM memory
      free(ptr, size + 4);

      return result;
    } catch (error) {
      console.error("Error reading sized buffer:", error);
      // Make sure to free memory even if an error occurs
      try {
        if (size > 0) {
          free(ptr, size + 4);
        } else {
          free(ptr, 4); // Free at least the header if we couldn't read the size
        }
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

    // Validate all keys first before allocating memory
    const decodedKeys = [];
    for (let i = 0; i < keyList.length; i++) {
      try {
        const key = Uint8Array.fromBase64(keyList[i]);
        if (!key || key.length !== 32) {
          throw new Error(
            `Key at index ${i} must be 32 bytes, got ${key ? key.length : "undefined"}`,
          );
        }
        decodedKeys.push(key);
      } catch (error) {
        throw new Error(`Invalid key at index ${i}: ${error.message}`);
      }
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
      for (let i = 0; i < decodedKeys.length; i++) {
        memoryView.set(decodedKeys[i], keysPtr + i * 32);
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
    if (typeof str !== "string") {
      throw new Error("Input must be a string");
    }
    return new TextEncoder().encode(str);
  }

  // Uint8Array to String conversion with null byte truncation
  function uint8ArrayToString(array) {
    if (!(array instanceof Uint8Array)) {
      throw new Error("Input must be a Uint8Array");
    }

    // Find the first null byte and truncate there
    const nullIndex = array.indexOf(0);
    if (nullIndex !== -1) {
      array = array.slice(0, nullIndex);
    }

    // If the array is empty or only contains null bytes, return empty string
    if (array.length === 0) {
      return "";
    }

    return new TextDecoder().decode(array);
  }

  // Input validation helpers
  function validateKeyList(keyList, name) {
    if (!Array.isArray(keyList) || keyList.length === 0) {
      throw new Error(`${name} must be a non-empty array`);
    }
    if (keyList.length > 1000) {
      // Reasonable limit
      throw new Error(`${name} list too large: ${keyList.length} keys`);
    }
  }

  function validateMessage(message, maxSize) {
    if (!message || typeof message !== "string") {
      throw new Error("Message must be a non-empty string");
    }
    if (message.length === 0) {
      throw new Error("Message cannot be empty");
    }
    if (message.length > maxSize) {
      throw new Error(
        `Message is bigger than max_size: ${message.length} > ${maxSize}`,
      );
    }
  }

  function validatePositiveInteger(value, name, max = 1000000) {
    if (!Number.isInteger(value) || value <= 0 || value > max) {
      throw new Error(
        `${name} must be a positive integer <= ${max}, got: ${value}`,
      );
    }
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
          secretKey: result.slice(0, 32).toBase64({ omitPadding: true }),
          publicKey: result.slice(32, 64).toBase64({ omitPadding: true }),
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
          secretKey: result.slice(0, 32).toBase64({ omitPadding: true }),
          publicKey: result.slice(32, 64).toBase64({ omitPadding: true }),
        };
      } catch (error) {
        console.error("Error generating trustee key pair:", error);
        throw new Error(
          "Failed to generate trustee key pair: " + error.message,
        );
      }
    },

    encrypt: (mixnetPublicKeyList, trusteePublicKeyList, message, maxSize) => {
      let mixnetKeys = null;
      let trusteeKeys = null;
      let messageData = null;

      try {
        // Input validation
        validateKeyList(mixnetPublicKeyList, "Mixnet public key list");
        validateKeyList(trusteePublicKeyList, "Trustee public key list");
        validatePositiveInteger(maxSize, "maxSize");
        validateMessage(message, maxSize);

        // Convert string message to Uint8Array
        const messageBytes = stringToUint8Array(message);

        // Prepare mixnet keys
        mixnetKeys = copyKeysToWasm(mixnetPublicKeyList);

        // Prepare trustee keys
        trusteeKeys = copyKeysToWasm(trusteePublicKeyList);

        // Prepare message
        messageData = copyToWasm(messageBytes);

        // Call WASM encrypt function
        const resultPtr = wasm_encrypt(
          mixnetKeys.count,
          trusteeKeys.count,
          mixnetKeys.ptr,
          trusteeKeys.ptr,
          messageData.ptr,
          messageData.size,
          maxSize,
        );

        if (!resultPtr) {
          throw new Error("Encryption failed");
        }

        // Read the result (sized buffer)
        const encryptedData = readSizedBuffer(resultPtr);

        // Get cypher size to split the result
        const cypherSize = wasm_cypher_size(mixnetKeys.count, maxSize);
        if (cypherSize === 0) {
          throw new Error("Invalid cypher size");
        }

        if (encryptedData.length < cypherSize * 2) {
          throw new Error("Encrypted data too small");
        }

        // Split the data into cyphers and control data
        const cypher1 = encryptedData.slice(0, cypherSize);
        const cypher2 = encryptedData.slice(cypherSize, cypherSize * 2);
        const controlData = encryptedData.slice(cypherSize * 2);

        return {
          cyphers: [cypher1, cypher2],
          controlData: controlData,
        };
      } catch (error) {
        console.error("Error during encryption:", error);
        throw new Error("Encryption failed: " + error.message);
      } finally {
        // Clean up allocated memory - WASM function handles this now
        // but we keep this for safety in case of early errors
      }
    },

    decrypt_mixnet: (secretKey, cypherBlock, cypherCount) => {
      let keyPtr = null;
      let cypherData = null;

      try {
        // Input validation
        if (!secretKey || typeof secretKey !== "string") {
          throw new Error("Secret key must be a non-empty string");
        }
        if (!(cypherBlock instanceof Uint8Array) || cypherBlock.length === 0) {
          throw new Error("Cypher block must be a non-empty Uint8Array");
        }
        validatePositiveInteger(cypherCount, "cypherCount");

        if (cypherBlock.length % cypherCount !== 0) {
          throw new Error("Cypher block size not divisible by cypher count");
        }

        // Allocate and copy the secret key
        keyPtr = alloc(32);
        if (!keyPtr) {
          throw new Error("Failed to allocate memory for secret key");
        }

        const keyView = new Uint8Array(memory.buffer, keyPtr, 32);
        const key = Uint8Array.fromBase64(secretKey);
        if (key.length !== 32) {
          throw new Error("Secret key must be 32 bytes");
        }
        keyView.set(key);

        // Copy the cypher block
        cypherData = copyToWasm(cypherBlock);

        // Call WASM decrypt_mixnet function
        const resultPtr = wasm_decrypt_mixnet(
          keyPtr,
          cypherCount,
          cypherData.ptr,
          cypherData.size,
        );

        // WASM function now handles cleanup of inputs
        keyPtr = null;
        cypherData = null;

        if (!resultPtr) {
          throw new Error("Mixnet decryption failed");
        }

        // Read the result (sized buffer)
        return readSizedBuffer(resultPtr);
      } catch (error) {
        // Clean up allocated memory on error
        if (keyPtr) {
          free(keyPtr, 32);
        }
        if (cypherData) {
          free(cypherData.ptr, cypherData.size);
        }
        console.error("Error during mixnet decryption:", error);
        throw new Error("Mixnet decryption failed: " + error.message);
      }
    },

    decrypt_trustee: (secretKeyList, cypherBlock, cypherCount) => {
      let trusteeKeys = null;
      let cypherData = null;

      try {
        // Input validation
        validateKeyList(secretKeyList, "Secret key list");
        if (!(cypherBlock instanceof Uint8Array) || cypherBlock.length === 0) {
          throw new Error("Cypher block must be a non-empty Uint8Array");
        }
        validatePositiveInteger(cypherCount, "cypherCount");

        if (cypherBlock.length % cypherCount !== 0) {
          throw new Error("Cypher block size not divisible by cypher count");
        }

        // Prepare trustee secret keys
        trusteeKeys = copyKeysToWasm(secretKeyList);

        // Copy the cypher block
        cypherData = copyToWasm(cypherBlock);

        // Call WASM decrypt_trustee function
        const resultPtr = wasm_decrypt_trustee(
          trusteeKeys.count,
          trusteeKeys.ptr,
          cypherCount,
          cypherData.ptr,
          cypherData.size,
        );

        // WASM function now handles cleanup of inputs
        trusteeKeys = null;
        cypherData = null;

        if (!resultPtr) {
          throw new Error("Trustee decryption failed");
        }

        // Read the result (sized buffer) and convert to array of strings
        const decryptedData = readSizedBuffer(resultPtr);

        // Parse decrypted data into individual messages
        // If cypherCount is 1, we assume the entire buffer is a single message
        if (cypherCount === 1) {
          return [uint8ArrayToString(decryptedData)];
        }

        if (decryptedData.length % cypherCount !== 0) {
          throw new Error("Decrypted data size not divisible by cypher count");
        }

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
        // Clean up allocated memory on error
        if (trusteeKeys) {
          free(trusteeKeys.ptr, trusteeKeys.count * 32);
        }
        if (cypherData) {
          free(cypherData.ptr, cypherData.size);
        }
        console.error("Error during trustee decryption:", error);
        throw new Error("Trustee decryption failed: " + error.message);
      }
    },

    validate: (
      encryptResultList,
      mixnetDataList,
      mixnetPublicKeyList,
      trusteePublicKeyList,
      trusteeSecretKeyList,
      maxSize,
      userCount,
    ) => {
      let userDataPtr = null;
      let mixnetSizePtr = null;
      let mixnetDataPtr = null;
      let mixnetKeys = null;
      let trusteePublicKeys = null;
      let trusteeSecretKeys = null;

      try {
        // Input validation
        if (
          !Array.isArray(encryptResultList) ||
          encryptResultList.length === 0
        ) {
          throw new Error("Encrypt result list must be a non-empty array");
        }
        if (!Array.isArray(mixnetDataList) || mixnetDataList.length === 0) {
          throw new Error("Mixnet data list must be a non-empty array");
        }
        validateKeyList(mixnetPublicKeyList, "Mixnet public key list");
        validateKeyList(trusteePublicKeyList, "Trustee public key list");
        validateKeyList(trusteeSecretKeyList, "Trustee secret key list");
        validatePositiveInteger(maxSize, "maxSize");
        validatePositiveInteger(userCount, "userCount");

        if (encryptResultList.length !== userCount) {
          throw new Error("Encrypt result list length must match user count");
        }

        // Convert encryptResultList to userDataBlock by concatenating all entries
        let totalSize = 0;
        for (const encryptResult of encryptResultList) {
          if (
            !encryptResult.cyphers ||
            !Array.isArray(encryptResult.cyphers) ||
            encryptResult.cyphers.length !== 2
          ) {
            throw new Error("Each encrypt result must have exactly 2 cyphers");
          }
          if (!encryptResult.controlData) {
            throw new Error("Each encrypt result must have control data");
          }
          totalSize += encryptResult.cyphers[0].length;
          totalSize += encryptResult.cyphers[1].length;
          totalSize += encryptResult.controlData.length;
        }

        const userDataBlock = new Uint8Array(totalSize);
        let userOffset = 0;

        for (const encryptResult of encryptResultList) {
          // Copy first cypher
          userDataBlock.set(encryptResult.cyphers[0], userOffset);
          userOffset += encryptResult.cyphers[0].length;

          // Copy second cypher
          userDataBlock.set(encryptResult.cyphers[1], userOffset);
          userOffset += encryptResult.cyphers[1].length;

          // Copy control data
          userDataBlock.set(encryptResult.controlData, userOffset);
          userOffset += encryptResult.controlData.length;
        }

        // Copy user data block to WASM
        userDataPtr = copyToWasm(userDataBlock);

        // Prepare mixnet size list
        const mixnetSizeList = new Uint8Array(mixnetDataList.length * 4);
        for (let i = 0; i < mixnetDataList.length; i++) {
          if (!(mixnetDataList[i] instanceof Uint8Array)) {
            throw new Error(`Mixnet data at index ${i} must be a Uint8Array`);
          }
          const size = mixnetDataList[i].length;
          const view = new DataView(mixnetSizeList.buffer);
          view.setUint32(i * 4, size, true); // Little endian
        }
        mixnetSizePtr = copyToWasm(mixnetSizeList);

        // Concatenate mixnet data blocks
        const totalMixnetSize = mixnetDataList.reduce(
          (sum, data) => sum + data.length,
          0,
        );
        const mixnetDataBlock = new Uint8Array(totalMixnetSize);
        let mixnetOffset = 0;
        for (const data of mixnetDataList) {
          mixnetDataBlock.set(data, mixnetOffset);
          mixnetOffset += data.length;
        }
        mixnetDataPtr = copyToWasm(mixnetDataBlock);

        // Prepare mixnet public keys
        mixnetKeys = copyKeysToWasm(mixnetPublicKeyList);

        // Prepare trustee public keys
        trusteePublicKeys = copyKeysToWasm(trusteePublicKeyList);

        // Prepare trustee secret keys
        trusteeSecretKeys = copyKeysToWasm(trusteeSecretKeyList);

        // Call WASM validate function
        const result = wasm_validate(
          userCount,
          trusteeSecretKeyList.length,
          userDataPtr.ptr,
          userDataPtr.size,
          maxSize,
          mixnetSizePtr.ptr,
          mixnetSizePtr.size,
          mixnetDataPtr.ptr,
          mixnetDataPtr.size,
          mixnetKeys.ptr,
          trusteePublicKeys.ptr,
          trusteeSecretKeys.ptr,
        );

        // WASM function now handles cleanup of inputs
        userDataPtr = null;
        mixnetSizePtr = null;
        mixnetDataPtr = null;
        mixnetKeys = null;
        trusteePublicKeys = null;
        trusteeSecretKeys = null;

        return result;
      } catch (error) {
        // Clean up allocated memory on error
        if (userDataPtr) {
          free(userDataPtr.ptr, userDataPtr.size);
        }
        if (mixnetSizePtr) {
          free(mixnetSizePtr.ptr, mixnetSizePtr.size);
        }
        if (mixnetDataPtr) {
          free(mixnetDataPtr.ptr, mixnetDataPtr.size);
        }
        if (mixnetKeys) {
          free(mixnetKeys.ptr, mixnetKeys.count * 32);
        }
        if (trusteePublicKeys) {
          free(trusteePublicKeys.ptr, trusteePublicKeys.count * 32);
        }
        if (trusteeSecretKeys) {
          free(trusteeSecretKeys.ptr, trusteeSecretKeys.count * 32);
        }
        console.error("Error during validation:", error);
        throw new Error("Validation failed: " + error.message);
      }
    },
  };
}
