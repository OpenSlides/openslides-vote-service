/**
 * CryptoVote Wrapper - WebWorker-based WASM Module Interface
 *
 * This wrapper provides a clean interface to the WASM crypto vote module
 * running in a WebWorker to prevent blocking the main thread.
 */

function CryptoVoteWrapper() {
  this.worker = null;
  this.isInitialized = false;
  this.messageId = 0;
  this.pendingMessages = new Map();
  this.callbacks = {
    onPublishKeyPublic: null,
    onPublishVote: null,
    onSetCanVote: null,
    onLog: null,
  };
}

/**
 * Initialize the WASM module with callbacks
 * @param {string} wasmUrl - URL to the WASM file
 * @param {Object} callbacks - Callback functions
 * @param {Function} callbacks.onPublishKeyPublic - Called when public key needs to be published
 * @param {Function} callbacks.onPublishVote - Called when vote needs to be published
 * @param {Function} callbacks.onSetCanVote - Called when voting status changes
 * @param {Function} callbacks.onLog - Called for logging messages
 */
CryptoVoteWrapper.prototype.init = async function (wasmUrl, callbacks) {
  if (this.isInitialized) {
    throw new Error("CryptoVote wrapper already initialized");
  }

  // Validate callbacks
  callbacks = callbacks || {};
  this.callbacks = {
    onPublishKeyPublic: callbacks.onPublishKeyPublic || null,
    onPublishVote: callbacks.onPublishVote || null,
    onSetCanVote: callbacks.onSetCanVote || null,
    onLog:
      callbacks.onLog ||
      function (message) {
        console.log("CryptoVote: " + message);
      },
  };

  try {
    // Create and setup worker
    this.worker = new Worker("/crypto_vote_worker.js");
    this.setupWorkerMessageHandling();

    // Load WASM file
    var response = await fetch(wasmUrl);
    if (!response.ok) {
      throw new Error(
        "Failed to fetch WASM file: " +
          response.status +
          " " +
          response.statusText,
      );
    }

    var wasmArrayBuffer = await response.arrayBuffer();

    // Initialize WASM in worker
    var result = await this.sendMessage("init", {
      wasmArrayBuffer: wasmArrayBuffer,
    });

    if (!result.success) {
      throw new Error(result.error || "Failed to initialize WASM module");
    }

    this.isInitialized = true;
    this.callbacks.onLog("CryptoVote wrapper initialized successfully");
  } catch (error) {
    this.cleanup();
    throw new Error(
      "Failed to initialize CryptoVote wrapper: " + error.message,
    );
  }
};

/**
 * Start the crypto vote application
 * @param {number} userId - User ID to start the application with
 */
CryptoVoteWrapper.prototype.start = async function (userId) {
  this.ensureInitialized();

  if (!userId || typeof userId !== "number" || userId < 1) {
    throw new Error("Invalid user ID: must be a positive number");
  }

  try {
    var result = await this.sendMessage("start", { userId: userId });

    if (!result.success) {
      throw new Error(result.error || "Failed to start application");
    }

    this.callbacks.onLog("Application started with user ID: " + userId);
    return result;
  } catch (error) {
    throw new Error("Failed to start application: " + error.message);
  }
};

/**
 * Process an incoming event
 * @param {string} eventData - Event data as string
 */
CryptoVoteWrapper.prototype.processEvent = async function (eventData) {
  this.ensureInitialized();

  if (typeof eventData !== "string") {
    throw new Error("Event data must be a string");
  }

  try {
    var result = await this.sendMessage("process_event", {
      eventData: eventData,
    });

    if (!result.success) {
      throw new Error(result.error || "Failed to process event");
    }

    return result;
  } catch (error) {
    this.callbacks.onLog("Error processing event: " + error.message);
    throw error;
  }
};

/**
 * Encrypt and send a vote
 * @param {string} voteData - Vote data as string
 */
CryptoVoteWrapper.prototype.encryptAndSendVote = async function (voteData) {
  this.ensureInitialized();

  if (typeof voteData !== "string") {
    throw new Error("Vote data must be a string");
  }

  try {
    var result = await this.sendMessage("encrypt_and_send_vote", {
      voteData: voteData,
    });

    if (!result.success) {
      throw new Error(result.error || "Failed to encrypt and send vote");
    }

    this.callbacks.onLog("Vote encrypted and sent successfully");
    return result;
  } catch (error) {
    throw new Error("Failed to encrypt and send vote: " + error.message);
  }
};

/**
 * Cleanup resources
 */
CryptoVoteWrapper.prototype.destroy = function () {
  this.cleanup();
  this.callbacks.onLog("CryptoVote wrapper destroyed");
};

// Private methods

CryptoVoteWrapper.prototype.ensureInitialized = function () {
  if (!this.isInitialized || !this.worker) {
    throw new Error("CryptoVote wrapper not initialized. Call init() first.");
  }
};

CryptoVoteWrapper.prototype.setupWorkerMessageHandling = function () {
  var self = this;

  this.worker.addEventListener("message", function (event) {
    var data = event.data;
    var type = data.type;
    var id = data.id;
    var callbackType = data.callbackType;
    var callbackId = data.callbackId;
    var eventData = data.data;
    var success = data.success;
    var error = data.error;

    switch (type) {
      case "response":
      case "error":
        self.handleWorkerResponse(id, success, eventData, error);
        break;

      case "callback":
        if (callbackId) {
          // Handle callback with response expected
          self.handleCallbackWithResponse(callbackType, callbackId, eventData);
        } else {
          // Handle fire-and-forget callback
          self.handleCallback(callbackType, eventData);
        }
        break;

      default:
        self.callbacks.onLog("Unknown message type from worker: " + type);
    }
  });

  this.worker.addEventListener("error", function (event) {
    self.callbacks.onLog("Worker error: " + event.message);
  });
};

CryptoVoteWrapper.prototype.handleWorkerResponse = function (
  id,
  success,
  data,
  error,
) {
  var pendingMessage = this.pendingMessages.get(id);
  if (!pendingMessage) {
    return; // Message might have timed out
  }

  this.pendingMessages.delete(id);
  clearTimeout(pendingMessage.timeout);

  if (success) {
    pendingMessage.resolve(data);
  } else {
    pendingMessage.reject(new Error(error || "Unknown worker error"));
  }
};

CryptoVoteWrapper.prototype.handleCallback = function (callbackType, data) {
  try {
    switch (callbackType) {
      case "log":
        if (this.callbacks.onLog) {
          this.callbacks.onLog(data.message);
        }
        break;

      case "set_can_vote":
        if (this.callbacks.onSetCanVote) {
          this.callbacks.onSetCanVote(data.canVote, data.size);
        }
        break;

      default:
        this.callbacks.onLog("Unknown callback type: " + callbackType);
    }
  } catch (error) {
    this.callbacks.onLog("Error in callback handler: " + error.message);
  }
};

CryptoVoteWrapper.prototype.handleCallbackWithResponse = async function (
  callbackType,
  callbackId,
  data,
) {
  var result = null;
  var error = null;

  try {
    switch (callbackType) {
      case "publish_key_public":
        if (this.callbacks.onPublishKeyPublic) {
          result = await this.callbacks.onPublishKeyPublic(data.key);
        } else {
          throw new Error("No onPublishKeyPublic callback provided");
        }
        break;

      case "publish_vote":
        if (this.callbacks.onPublishVote) {
          result = await this.callbacks.onPublishVote(data.vote);
        } else {
          throw new Error("No onPublishVote callback provided");
        }
        break;

      default:
        throw new Error("Unknown callback type: " + callbackType);
    }
  } catch (err) {
    error = err.message;
  }

  // Send response back to worker
  this.worker.postMessage({
    type: "callback",
    callbackId: callbackId,
    result: result,
    error: error,
  });
};

CryptoVoteWrapper.prototype.sendMessage = function (type, data) {
  var self = this;
  return new Promise(function (resolve, reject) {
    var id = ++self.messageId;

    var timeout = setTimeout(function () {
      self.pendingMessages.delete(id);
      reject(new Error("Message timeout: " + type));
    }, 30000); // 30 second timeout

    self.pendingMessages.set(id, {
      resolve: resolve,
      reject: reject,
      timeout: timeout,
    });

    self.worker.postMessage({
      type: type,
      id: id,
      data: data,
    });
  });
};

CryptoVoteWrapper.prototype.cleanup = function () {
  var self = this;

  // Clear pending messages
  this.pendingMessages.forEach(function (pending, id) {
    clearTimeout(pending.timeout);
    pending.reject(new Error("Worker terminated"));
  });
  this.pendingMessages.clear();

  // Terminate worker
  if (this.worker) {
    this.worker.terminate();
    this.worker = null;
  }

  this.isInitialized = false;
};

// Make CryptoVoteWrapper available globally
if (typeof window !== "undefined") {
  window.CryptoVoteWrapper = CryptoVoteWrapper;
} else {
  console.log("Window object not available, CryptoVoteWrapper not registered");
}
