const std = @import("std");
const builtin = @import("builtin");
const crypto = @import("crypto.zig");

/// Global allocator for WebAssembly memory management.
/// Uses the WASM allocator in production, testing allocator during tests.
const global_allocator = if (builtin.is_test) std.testing.allocator else std.heap.wasm_allocator;

/// Allocates memory in WebAssembly linear memory.
/// This function is exported and can be called from the host environment.
///
/// Args:
///   size: Number of bytes to allocate
///
/// Returns:
///   ?[*]u8: Pointer to allocated memory, or null if allocation fails
export fn alloc(size: u32) ?[*]u8 {
    const buf = global_allocator.alloc(u8, size) catch return null;
    return buf.ptr;
}

/// Frees previously allocated memory in WebAssembly linear memory.
/// This function is exported and can be called from the host environment.
///
/// Args:
///   ptr: Pointer to memory to free
///   size: Size of the memory block to free
export fn free(ptr: [*]u8, size: u32) void {
    global_allocator.free(ptr[0..size]);
}

/// Host environment functions that must be provided by the WebAssembly runtime.
/// These functions allow the WASM module to interact with the host environment.
const Env = struct {
    /// Logs a message to the host console.
    extern fn console_log(ptr: [*]u8, len: usize) void;

    /// Fills a buffer with cryptographically secure random bytes from the host.
    extern fn get_random(ptr: [*]u8, amount: u32) void;
};

/// Logs a formatted message to the host console.
/// This function provides printf-style formatting for debugging and error reporting.
///
/// Args:
///   allocator: A area allocator. consoleLog does not free the used memory.
///   fmt: Format string (compile-time known)
///   args: Arguments for the format string
pub fn consoleLog(allocator: std.mem.Allocator, comptime fmt: []const u8, args: anytype) void {
    const msg = std.fmt.allocPrint(allocator, fmt, args) catch unreachable;
    Env.console_log(msg.ptr, msg.len);
}

/// Fills a buffer with cryptographically secure random bytes from the host.
/// This function bridges Zig's random API with the host environment.
///
/// Args:
///   buf: Buffer to fill with random bytes
fn getRandom(buf: []u8) void {
    Env.get_random(buf.ptr, buf.len);
}

/// Generates a cryptographic key pair for a mixnet node.
/// Mixnet nodes are responsible for anonymizing messages by shuffling and re-encryption.
///
/// Returns:
///   ?[*]const u8: Pointer to 64-byte key pair (32 bytes secret + 32 bytes public)
///                 Returns null on allocation failure
///
/// Memory Layout:
///   Bytes 0-31:  Secret key (32 bytes)
///   Bytes 32-63: Public key (32 bytes)
///
/// Note: The caller is responsible for freeing the returned memory using free()
export fn gen_mixnet_key_pair() ?[*]const u8 {
    const kp = crypto.KeyPairMixnet.generate(getRandom);

    const result = global_allocator.alloc(u8, 64) catch return null;
    @memcpy(result[0..32], &kp.key_secret);
    @memcpy(result[32..][0..32], &kp.key_public);
    return result.ptr;
}

/// Generates a cryptographic key pair for a trustee.
/// Trustees collectively hold the final decryption keys for the voting system.
///
/// Returns:
///   ?[*]const u8: Pointer to 64-byte key pair (32 bytes secret + 32 bytes public)
///                 Returns null on allocation failure
///
/// Memory Layout:
///   Bytes 0-31:  Secret key (32 bytes Ed25519 scalar)
///   Bytes 32-63: Public key (32 bytes Ed25519 point)
///
/// Note: The caller is responsible for freeing the returned memory using free()
export fn gen_trustee_key_pair() ?[*]const u8 {
    const kp = crypto.KeyPairTrustee.generate(getRandom);

    const result = global_allocator.alloc(u8, 64) catch return null;
    @memcpy(result[0..32], &kp.key_secret);
    @memcpy(result[32..][0..32], &kp.key_public);
    return result.ptr;
}

/// Calculates the size of a single encrypted message (cypher).
/// This function is necessary to split the result of encrypt() into individual components.
///
/// Args:
///   mixnet_count: Number of mixnet nodes in the encryption chain
///   max_size: Maximum size of messages (used for padding calculation)
///
/// Returns:
///   u32: Size in bytes of one encrypted message
///        Returns 0 if inputs are invalid
///
/// Note: Each encryption layer adds overhead for ephemeral keys and authentication tags
export fn cypher_size(
    mixnet_count: u32,
    max_size: u32,
) u32 {
    if (mixnet_count == 0) return 0;
    if (max_size == 0) return 0;
    return @intCast(crypto.calcCypherSize(max_size, mixnet_count));
}

/// Encrypts a voting message for anonymity and integrity protection.
/// Creates both a real encrypted message and a fake one to provide deniability.
/// The encrypted data goes through multiple layers: first trustees, then mixnet nodes.
///
/// Encryption Flow:
///   1. Message → Trustee encryption (threshold cryptography)
///   2. Result → Mixnet layer N encryption
///   3. Result → Mixnet layer N-1 encryption
///   4. ... (continue for all mixnet layers in reverse order)
///   5. Result → Mixnet layer 1 encryption
///
/// Args:
///   mixnet_count: Number of mixnet nodes in the anonymization chain
///   trustee_count: Number of trustees in the threshold decryption group
///   mixnet_key_public_ptr: Pointer to array of mixnet public keys (32 bytes each)
///   trustee_key_public_ptr: Pointer to array of trustee public keys (32 bytes each)
///   msg_ptr: Pointer to the message to encrypt
///   msg_len: Length of the message in bytes
///   max_size: Maximum message size (used for padding to fixed length)
///
/// Returns:
///   ?[*]u8: Pointer to encrypted result with size prefix, or null on error
///
/// Result Format:
///   Bytes 0-3:    Size of following data (little-endian u32)
///   Bytes 4-N:    First cypher (real or fake, order randomized)
///   Bytes N+1-M:  Second cypher (fake or real, order randomized)
///   Bytes M+1-End: Control data (encrypted seed for fake message verification)
///
/// Error Conditions:
///   - Invalid mixnet or trustee counts (zero)
///   - Invalid key pointers or message parameters
///   - Message larger than max_size
///   - Memory allocation failures
///   - Cryptographic operation failures
///
/// Note: The caller is responsible for freeing input memory and the returned memory
export fn encrypt(
    mixnet_count: u32,
    trustee_count: u32,
    mixnet_key_public_ptr: [*]const [32]u8,
    trustee_key_public_ptr: [*]const [32]u8,
    msg_ptr: [*]const u8,
    msg_len: u32,
    max_size: u32,
) ?[*]u8 {
    var arena: std.heap.ArenaAllocator = .init(global_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    if (!validateMessage(msg_ptr, msg_len, max_size)) {
        consoleLog(allocator, "Invalid message parameters", .{});
        return null;
    }

    // Create read-only slices without taking ownership
    const message = msg_ptr[0..msg_len];
    const mixnet_key_public_list: []const [32]u8 = mixnet_key_public_ptr[0..mixnet_count];
    const trustee_key_public_list: []const [32]u8 = trustee_key_public_ptr[0..trustee_count];

    const result = crypto.encryptMessage(
        allocator,
        getRandom,
        mixnet_key_public_list,
        trustee_key_public_list,
        message,
        max_size,
    ) catch |err| {
        consoleLog(allocator, "Error encrypt_message: {}", .{err});
        return null;
    };

    return result.toBytesWithPrefix(global_allocator) catch return null;
}

/// Decrypts a block of encrypted messages using a mixnet node's secret key.
/// This function is called once for each mixnet node in the decryption chain.
/// The messages are processed in parallel and sorted to remove ordering information.
///
/// Decryption Flow:
///   - First mixnet node: Processes output from encrypt() (without size prefix)
///   - Subsequent nodes: Process output from previous decrypt_mixnet() call
///   - Final output: Goes to decrypt_trustee() for final decryption
///
/// Args:
///   key_secret: Pointer to the mixnet node's 32-byte secret key
///   cypher_count: Number of encrypted messages in the block
///   cypher_block_ptr: Pointer to concatenated encrypted messages
///   cypher_block_size: Total size of the encrypted block in bytes
///
/// Returns:
///   ?[*]u8: Pointer to decrypted block with size prefix, or null on error
///
/// Result Format:
///   Bytes 0-3: Size of following data (little-endian u32)
///   Bytes 4-N: Concatenated decrypted messages (sorted lexicographically)
///
/// Error Conditions:
///   - Invalid cypher count (zero)
///   - Invalid cypher block size or pointer
///   - Block size not divisible by cypher count (indicates malformed data)
///   - Individual cypher size is zero
///   - Cryptographic decryption failures
///   - Memory allocation failures
///
/// Security Note:
///   Messages are sorted lexicographically to remove ordering information,
///   which is crucial for maintaining voter anonymity in the mixnet.
///
/// Note: The caller is responsible for freeing input memory and the returned memory
export fn decrypt_mixnet(
    key_secret: *const [32]u8,
    cypher_count: u32,
    cypher_block_ptr: [*]const u8,
    cypher_block_size: u32,
) ?[*]u8 {
    var arena: std.heap.ArenaAllocator = .init(global_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    if (cypher_count == 0) {
        consoleLog(allocator, "Invalid cypher count", .{});
        return null;
    }
    if (cypher_block_size == 0) {
        consoleLog(allocator, "Invalid cypher block", .{});
        return null;
    }
    if (cypher_block_size % cypher_count != 0) {
        consoleLog(allocator, "Cypher block size not divisible by cypher count", .{});
        return null;
    }

    // Create read-only slice without taking ownership
    const cypher_block = cypher_block_ptr[0..cypher_block_size];

    const decrypted = crypto.decryptMixnet(
        allocator,
        key_secret.*,
        cypher_count,
        cypher_block,
    ) catch |err| {
        consoleLog(allocator, "decrypt data: {}", .{err});
        return null;
    };

    return successSizedBuffer(global_allocator, decrypted);
}

/// Performs the final decryption of voting messages using trustee secret keys.
/// This function combines multiple trustee keys to decrypt messages that were
/// processed through the entire mixnet chain. Uses threshold cryptography.
///
/// Input Source:
///   The cypher block must be the output from decrypt_mixnet() of the final
///   mixnet node in the chain.
///
/// Args:
///   trustee_count: Number of trustees participating in decryption
///   key_secret_list: Pointer to array of trustee secret keys (32 bytes each)
///   cypher_count: Number of encrypted messages to decrypt
///   cypher_block_ptr: Pointer to the encrypted message block
///   cypher_block_size: Size of the encrypted block in bytes
///
/// Returns:
///   ?[*]u8: Pointer to decrypted messages with size prefix, or null on error
///
/// Result Format:
///   Bytes 0-3: Size of following data (little-endian u32)
///   Bytes 4-N: Concatenated decrypted voting messages
///
/// Message Extraction:
///   The result can be divided into chunks of (total_size / cypher_count)
///   to extract individual voting messages. Each message may be padded
///   with null bytes to reach the fixed max_size.
///
/// Error Conditions:
///   - Invalid trustee count or key list
///   - Invalid cypher count (zero)
///   - Invalid cypher block size or pointer
///   - Block size not divisible by cypher count
///   - Individual cypher size is zero
///   - Memory allocation failures
///   - Cryptographic decryption failures (invalid cyphers, authentication failures)
///
/// Security Note:
///   This function reveals the final voting messages, so it should only be
///   called after the voting period has ended and by authorized entities.
///
/// Note: The caller is responsible for freeing input memory and the returned memory
export fn decrypt_trustee(
    trustee_count: u32,
    key_secret_list: [*]const [32]u8,
    cypher_count: u32,
    cypher_block_ptr: [*]const u8,
    cypher_block_size: u32,
) ?[*]u8 {
    var arena: std.heap.ArenaAllocator = .init(global_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    if (cypher_count == 0) {
        consoleLog(allocator, "Invalid cypher count", .{});
        return null;
    }
    if (cypher_block_size == 0) {
        consoleLog(allocator, "Invalid cypher block", .{});
        return null;
    }
    if (cypher_block_size % cypher_count != 0) {
        consoleLog(allocator, "Cypher block size not divisible by cypher count", .{});
        return null;
    }

    // Create read-only slices without taking ownership
    const trustee_keys = key_secret_list[0..trustee_count];
    const cypher_block = cypher_block_ptr[0..cypher_block_size];

    const buf_size = crypto.decryptTrusteeBufSize(cypher_block_size, cypher_count);

    const buf = allocator.alloc(u8, buf_size) catch |err| {
        consoleLog(allocator, "Error allocating {} bytes of memory for decrypt buf: {}", .{ buf_size, err });
        return null;
    };

    const decrypted = crypto.decryptTrustee(
        trustee_keys,
        cypher_count,
        cypher_block,
        buf,
    ) catch |err| {
        consoleLog(allocator, "Error calling decrypt_trustee: {}", .{err});
        return null;
    };

    return successSizedBuffer(global_allocator, decrypted);
}

/// Validates the integrity of the entire voting process end-to-end.
/// Performs cryptographic verification that users submitted valid votes and
/// that mixnet nodes processed them correctly without tampering or manipulation.
///
/// Validation Process:
///   1. For each user: Decrypt control data to get fake message seed
///   2. Reconstruct fake encryption steps using the seed
///   3. Verify fake messages appear in user's submitted cyphers
///   4. Verify fake messages appear in each mixnet node's output
///   5. Return validation results indicating any detected fraud
///
/// Args:
///   user_count: Number of users who submitted votes
///   trustee_count: Number of trustees in the system
///   user_data_block_ptr: Pointer to all user-submitted encrypted votes
///   user_data_block_size: Size of user data block in bytes
///   max_size: Maximum message size (for padding validation)
///   mixnet_size_ptr: Pointer to array of mixnet output sizes (4 bytes each)
///   mixnet_size_len: Length of mixnet size array in bytes
///   mixnet_data_block_ptr: Pointer to concatenated mixnet outputs
///   mixnet_data_block_size: Size of mixnet data block in bytes
///   mixnet_key_public_ptr: Pointer to array of mixnet public keys
///   trustee_key_public_ptr: Pointer to array of trustee public keys
///   trustee_key_secret_ptr: Pointer to array of trustee secret keys
///
/// Returns:
///   i32: Validation result code
///     0: All validations passed successfully
///    -N: User N-1 submitted invalid/fraudulent data (N >= 1)
///    +N: Mixnet node N-1 tampered with data (N >= 1)
///  -1000: Critical validation error (invalid inputs, memory failures, etc.)
///
/// Error Conditions:
///   - Invalid user count, trustee count, or size parameters
///   - User data block size not divisible by user count
///   - Mixnet size array not properly formatted (not divisible by 4)
///   - Invalid structure
///   - Max size too large (>1MB) or zero
///   - Memory allocation failures
///   - Cryptographic operation failures
///   - Mismatched data sizes or counts
///
/// Security Properties:
///   - Detects if users submitted invalid fake message proofs
///   - Detects if mixnet nodes modified, deleted, or added messages
///   - Ensures all fake messages are properly propagated through the mixnet
///   - Validates cryptographic integrity at each step
///
/// Note: The caller is responsible for freeing all input memory
export fn validate(
    user_count: u32,
    trustee_count: u32,
    user_data_block_ptr: [*]const u8,
    user_data_block_size: u32,
    max_size: u32,
    mixnet_size_ptr: [*]const u8,
    mixnet_size_len: u32,
    mixnet_data_block_ptr: [*]const u8,
    mixnet_data_block_size: u32,
    mixnet_key_public_ptr: [*]const [32]u8,
    trustee_key_public_ptr: [*]const [32]u8,
    trustee_key_secret_ptr: [*]const [32]u8,
) i32 {
    var arena: std.heap.ArenaAllocator = .init(global_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    if (user_count == 0) {
        consoleLog(allocator, "Invalid user count", .{});
        return -1000;
    }
    if (trustee_count == 0) {
        consoleLog(allocator, "Invalid trustee count", .{});
        return -1000;
    }
    if (user_data_block_size == 0) {
        consoleLog(allocator, "Invalid user data block", .{});
        return -1000;
    }
    if (user_data_block_size % user_count != 0) {
        consoleLog(allocator, "User data block size not divisible by user count", .{});
        return -1000;
    }
    if (mixnet_size_len == 0) {
        consoleLog(allocator, "Invalid mixnet size data", .{});
        return -1000;
    }
    if (mixnet_size_len % 4 != 0) {
        consoleLog(allocator, "Mixnet size length not divisible by 4", .{});
        return -1000;
    }
    if (mixnet_data_block_size == 0) {
        consoleLog(allocator, "Invalid mixnet data block", .{});
        return -1000;
    }
    if (max_size == 0 or max_size > 1024 * 1024) {
        consoleLog(allocator, "Invalid max size", .{});
        return -1000;
    }

    // Create read-only slices without taking ownership
    const user_data_block = user_data_block_ptr[0..user_data_block_size];
    const mixnet_data_block = mixnet_data_block_ptr[0..mixnet_data_block_size];
    const mixnet_size_list = mixnet_size_ptr[0..mixnet_size_len];

    const mixnet_data_list = convertMixnetData(allocator, mixnet_size_list, mixnet_data_block) catch return -1000;

    // Validate key counts match data counts
    const mixnet_key_public_list = mixnet_key_public_ptr[0..mixnet_data_list.len];
    const trustee_key_public_list = trustee_key_public_ptr[0..trustee_count];
    const trustee_key_secret_list = trustee_key_secret_ptr[0..trustee_count];

    const result = crypto.validate(
        allocator,
        user_data_block,
        mixnet_data_list,
        mixnet_key_public_list,
        trustee_key_public_list,
        trustee_key_secret_list,
        max_size,
        user_count,
    ) catch |err| {
        consoleLog(allocator, "Error validate: {}", .{err});
        return -1000;
    };

    return result;
}

/// Converts mixnet size and data information into structured data arrays.
/// Parses the serialized mixnet output format into individual data blocks.
///
/// Args:
///   mixnet_size_list: Array of u32 sizes (serialized as bytes, little-endian)
///   mixnet_data_block: Concatenated data from all mixnet nodes
///
/// Returns:
///   [][]const u8: Array of slices pointing to individual mixnet data blocks
///   error.WrongInput: If input format is invalid
///   error.OutOfMemory: If memory allocation fails
///
/// Input Format:
///   mixnet_size_list: [size1 (4 bytes)] [size2 (4 bytes)] ... [sizeN (4 bytes)]
///   mixnet_data_block: [data1 (size1 bytes)] [data2 (size2 bytes)] ... [dataN (sizeN bytes)]
///
/// Validation:
///   - Size list length must be divisible by 4 (u32 size)
///   - Must have at least one mixnet node
///   - Data block must contain enough bytes for all specified sizes
fn convertMixnetData(
    allocator: std.mem.Allocator,
    mixnet_size_list: []const u8,
    mixnet_data_block: []const u8,
) ![][]const u8 {
    if (mixnet_size_list.len % 4 != 0) {
        return error.WrongInput;
    }

    const mixnet_count = mixnet_size_list.len / 4;
    if (mixnet_count == 0) {
        return error.WrongInput;
    }

    const u32_data = try allocator.alloc(u32, mixnet_count);
    for (0..mixnet_count) |i| {
        u32_data[i] = std.mem.readInt(u32, mixnet_size_list[i * 4 ..][0..4], .little);
    }

    const mixnet_data_list = try allocator.alloc([]const u8, mixnet_count);
    var offset: u32 = 0;
    for (0..mixnet_count) |i| {
        const size = u32_data[i];

        if (offset + size > mixnet_data_block.len) {
            return error.WrongInput;
        }

        mixnet_data_list[i] = mixnet_data_block[offset..][0..size];
        offset += size;
    }

    return mixnet_data_list;
}

/// Creates a buffer with a 4-byte size prefix for returning data to the host.
/// This is the standard format for variable-length data returned from WASM functions.
///
/// Args:
///  allocator: Memory allocator to use
///   buf: Data buffer to wrap with size prefix
///
/// Returns:
///   ?[*]u8: Pointer to new buffer with format [size (4 bytes)][data], or null on allocation failure
///
/// Output Format:
///   Bytes 0-3: Data length as little-endian u32 (excluding these 4 bytes)
///   Bytes 4-N: Original buffer data
///
/// Note: The caller is responsible for freeing the returned memory
fn successSizedBuffer(allocator: std.mem.Allocator, buf: []const u8) ?[*]u8 {
    const result = allocator.alloc(u8, buf.len + 4) catch return null;
    std.mem.writeInt(u32, result[0..4], @intCast(buf.len), .little);
    @memcpy(result[4..], buf);
    return result.ptr;
}

/// Validates message parameters for encryption functions.
/// Ensures message length and size constraints are met.
///
/// Args:
///   msg_ptr: Pointer to the message (cannot be null in Zig)
///   msg_len: Length of the message in bytes
///   max_size: Maximum allowed message size
///
/// Returns:
///   bool: True if message parameters are valid
fn validateMessage(msg_ptr: [*]const u8, msg_len: u32, max_size: u32) bool {
    _ = msg_ptr; // ptr cannot be null in Zig, just suppress unused warning
    if (msg_len == 0) return false;
    if (max_size < msg_len) return false;
    if (max_size > 1024 * 1024) return false; // 1MB limit for safety
    return true;
}
