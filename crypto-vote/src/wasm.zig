const std = @import("std");
const builtin = @import("builtin");
const crypto = @import("crypto.zig");

pub const allocator = if (builtin.is_test) std.testing.allocator else std.heap.wasm_allocator;

export fn alloc(size: u32) ?[*]u8 {
    const buf = allocator.alloc(u8, size) catch return null;
    return buf.ptr;
}

export fn free(ptr: [*]u8, size: u32) void {
    allocator.free(ptr[0..size]);
}

const Env = struct {
    extern fn console_log(ptr: [*]u8, len: usize) void;
    extern fn get_random(ptr: [*]u8, amount: u32) void;
};

pub fn consoleLog(comptime fmt: []const u8, args: anytype) void {
    const msg = std.fmt.allocPrint(allocator, fmt, args) catch unreachable;
    defer allocator.free(msg);
    Env.console_log(msg.ptr, msg.len);
}

pub fn getRandom(buf: []u8) void {
    Env.get_random(buf.ptr, buf.len);
}

pub const std_options = std.Options{
    .cryptoRandomSeed = getRandom,
};

// Managed buffer with RAII pattern
const ManagedBuffer = struct {
    data: []u8,

    fn init(size: usize) !ManagedBuffer {
        return ManagedBuffer{
            .data = try allocator.alloc(u8, size),
        };
    }

    fn deinit(self: ManagedBuffer) void {
        allocator.free(self.data);
    }

    fn ptr(self: ManagedBuffer) [*]u8 {
        return self.data.ptr;
    }
};

// Helper to validate input parameters
fn validateInputs(comptime T: type, ptr: [*]const T, count: u32) bool {
    if (count == 0) return false;
    _ = ptr; // ptr cannot be null in Zig, just suppress unused warning
    return true;
}

// Helper to validate message parameters
fn validateMessage(msg_ptr: [*]const u8, msg_len: u32, max_size: u32) bool {
    _ = msg_ptr; // ptr cannot be null in Zig, just suppress unused warning
    if (msg_len == 0) return false;
    if (max_size < msg_len) return false;
    if (max_size > 1024 * 1024) return false; // 1MB limit for safety
    return true;
}

// gen_mixnet_key_pair creates a keypair for a mixnet member.
//
// On error, the function returns 0.
//
// On success, the function returns a pointer to memory, where the keypair is.
// The first 32 bytes of the keypair is the secret key. The second 32 bytes is the
// public key.
//
// The memory has to be deallocated from the caller.
export fn gen_mixnet_key_pair() ?[*]const u8 {
    const kp = crypto.KeyPairMixnet.generate();

    const result = allocator.alloc(u8, 64) catch return null;
    @memcpy(result[0..32], &kp.key_secret);
    @memcpy(result[32..][0..32], &kp.key_public);
    return result.ptr;
}

// gen_trustee_key_pair creates a keypair for a trustee member.
//
// On error, the function returns 0.
//
// On success, the function returns a pointer to memory, where the keypair is.
// The first 32 bytes of the keypair is the secret key. The second 32 bytes is the
// public key.
//
// The memory has to be deallocated from the caller.
export fn gen_trustee_key_pair() ?[*]const u8 {
    const kp = crypto.KeyPairTrustee.generate();

    const result = allocator.alloc(u8, 64) catch return null;
    @memcpy(result[0..32], &kp.key_secret);
    @memcpy(result[32..][0..32], &kp.key_public);
    return result.ptr;
}

// cypher_size returns the size of one cypher returned from `encrypt`.
//
// This function is necessary, to split the result of `encrypt`.
//
// It Returns 0 on error.
export fn cypher_size(
    mixnet_count: u32,
    max_size: u32,
) u32 {
    if (mixnet_count == 0) return 0;
    if (max_size == 0) return 0;
    return @intCast(crypto.calc_cypher_size(max_size, mixnet_count));
}

// encrypt encrypts a message for the mixnet and trustees.
//
// It returns the encrypted message, a fake encrypted message and encrypted
// control_data.
//
// First, the message gets encrypted for all trustees at once. Then the result
// gets encrypted for each member of the mixnet, once at a time. It starts with
// the last member of the mixnet, and ends with the first. So the final result
// is a message, that is encrypted many times. To decrypt it, it first has to be
// decrypted from mixnet1, then mixnet2 and so on. The value, that was decrypted
// from the last member of the mixnet has to be decrypted with the private keys
// from all trustees.
//
// The argument `mixnet_count` is the amount of members of the mixnet. The
// argument `trustee_count` the amount of members of the trustee group.
//
// The argument `mixnet_key_public_ptr` has to be a pointer, that points to a list
// of all mixnet keys. The argument `trustee_key_public_ptr` is the same for the
// trustee group.
//
// `msg_ptr` has to be a pointer to the message, that has to be encrypted. `msg_len`
// is the length of this message.
//
// `max_size` is the number, how big a message could be in theory. It is used to
// add padding to the message.
//
// The caller is responsible for deallocating the input memory.
//
// On error, the function returns 0.
//
// On success, the function returns a pointer to memory. The first four bytes of
// this memory is the size following memory (the four bytes are not included).
// The following memory is the encrypted message, a fake encrypted message
// followed by encrypted control data.
//
// To split the result, the function `cypher_size` has to be called. It returns
// the size of one cypher. The first cypher are the first `cypher_size` bytes,
// the second message is the second `cypher_size` bytes, and the rest of the
// result are the control data.
//
// The order of the first cypher and second cypher is random. So the caller can
// not know, which cypher encrypts the real message and which the fake message.
export fn encrypt(
    mixnet_count: u32,
    trustee_count: u32,
    mixnet_key_public_ptr: [*]const [32]u8,
    trustee_key_public_ptr: [*]const [32]u8,
    msg_ptr: [*]const u8,
    msg_len: u32,
    max_size: u32,
) ?[*]u8 {
    // Input validation
    if (!validateInputs([32]u8, mixnet_key_public_ptr, mixnet_count)) {
        consoleLog("Invalid mixnet keys", .{});
        return null;
    }
    if (!validateInputs([32]u8, trustee_key_public_ptr, trustee_count)) {
        consoleLog("Invalid trustee keys", .{});
        return null;
    }
    if (!validateMessage(msg_ptr, msg_len, max_size)) {
        consoleLog("Invalid message parameters", .{});
        return null;
    }

    // Create read-only slices without taking ownership
    const message = msg_ptr[0..msg_len];
    const mixnet_key_public_list: []const [32]u8 = mixnet_key_public_ptr[0..mixnet_count];
    const trustee_key_public_list: []const [32]u8 = trustee_key_public_ptr[0..trustee_count];

    const result = crypto.encrypt_message(
        allocator,
        mixnet_key_public_list,
        trustee_key_public_list,
        message,
        max_size,
    ) catch |err| {
        consoleLog("Error encrypt_message: {}", .{err});
        return null;
    };
    defer result.free(allocator);

    return result.toBytesWithPrefix(allocator) catch return null;
}

// decrypt_mixnet decrypts a block of cyphers with a private key from a mixnet
// member.
//
// For the first member of the mixnet, the input cypher has to be combined
// result for each message encrypted with `encrypt` without the size-prefix.
//
// For each other member of the mixnet, the input cypher is the output of
// `decrypt_mixnet` from the previous mixnet member.
//
// The argument `key_secret` is the private key of the mixnet member.
//
// The argument `cypher_count` is the amount of messages, that should be
// decrypted.
//
// The argument `cypher_block_ptr` is a pointer to the memory, where the cypher
// block can be found. The argument `cypher_block_size` is the size of the block.
//
// Returns 0 on error.
//
// On success, it returns a pointer to the decrypted data block, with a four
// byte prefix of the new size.
//
// The caller is responsible for deallocating input and output memory.
export fn decrypt_mixnet(
    key_secret: *const [32]u8,
    cypher_count: u32,
    cypher_block_ptr: [*]const u8,
    cypher_block_size: u32,
) ?[*]u8 {
    // Input validation
    if (cypher_count == 0) {
        consoleLog("Invalid cypher count", .{});
        return null;
    }
    if (cypher_block_size == 0) {
        consoleLog("Invalid cypher block", .{});
        return null;
    }
    if (cypher_block_size % cypher_count != 0) {
        consoleLog("Cypher block size not divisible by cypher count", .{});
        return null;
    }

    // Create read-only slice without taking ownership
    const cypher_block = cypher_block_ptr[0..cypher_block_size];

    // Ensure all cyphers have the same size
    const individual_cypher_size = cypher_block_size / cypher_count;
    if (individual_cypher_size == 0) {
        consoleLog("Invalid cypher size", .{});
        return null;
    }

    const decrypted = crypto.decrypt_mixnet(
        allocator,
        key_secret.*,
        cypher_count,
        cypher_block,
    ) catch |err| {
        consoleLog("decrypt data: {}", .{err});
        return null;
    };
    defer allocator.free(decrypted);

    return successSizedBuffer(decrypted);
}

// decrypt_trustee decrypts a block of cyphers with all keys from the trustees.
//
// The input has to be the cypher block returned from `decrypt_mixnet` from the
// last member of the mixnet.
//
// The argument `trustee_count` is the amount of trustees. The argument
// `key_secret_list` is a pointer to all secret keys from each trustee.
//
// The argument `cypher_count` is the amount of messages, that should be
// decrypted.
//
// The argument `cypher_block_ptr` is a pointer to the memory, where the cypher
// block can be found. The argument `cypher_block_size` is the size of the block.
//
// Returns 0 on error.
//
// On success, it returns a pointer to the decrypted data block, with a four
// byte prefix of the new size. It can be divided in chunks of `cypher_count`
// blocks to get the list of decrypted messages.
//
// The caller is responsible for deallocating input and output memory.
export fn decrypt_trustee(
    trustee_count: u32,
    key_secret_list: [*]const [32]u8,
    cypher_count: u32,
    cypher_block_ptr: [*]const u8,
    cypher_block_size: u32,
) ?[*]u8 {
    // Input validation
    if (!validateInputs([32]u8, key_secret_list, trustee_count)) {
        consoleLog("Invalid trustee keys", .{});
        return null;
    }
    if (cypher_count == 0) {
        consoleLog("Invalid cypher count", .{});
        return null;
    }
    if (cypher_block_size == 0) {
        consoleLog("Invalid cypher block", .{});
        return null;
    }
    if (cypher_block_size % cypher_count != 0) {
        consoleLog("Cypher block size not divisible by cypher count", .{});
        return null;
    }

    // Create read-only slices without taking ownership
    const trustee_keys = key_secret_list[0..trustee_count];
    const cypher_block = cypher_block_ptr[0..cypher_block_size];

    // Ensure all cyphers have the same size
    const individual_cypher_size = cypher_block_size / cypher_count;
    if (individual_cypher_size == 0) {
        consoleLog("Invalid cypher size", .{});
        return null;
    }

    const buf_size = crypto.decrypt_trustee_buf_size(cypher_block_size, cypher_count);

    var buf = ManagedBuffer.init(buf_size) catch |err| {
        consoleLog("Error allocating {} bytes of memory for decrypt buf: {}", .{ buf_size, err });
        return null;
    };
    defer buf.deinit();

    const decrypted = crypto.decrypt_trustee(
        trustee_keys,
        cypher_count,
        cypher_block,
        buf.data,
    ) catch |err| {
        consoleLog("Error calling decrypt_trustee: {}", .{err});
        return null;
    };

    return successSizedBuffer(decrypted);
}

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
    // Input validation
    if (user_count == 0) {
        consoleLog("Invalid user count", .{});
        return -1000;
    }
    if (trustee_count == 0) {
        consoleLog("Invalid trustee count", .{});
        return -1000;
    }
    if (user_data_block_size == 0) {
        consoleLog("Invalid user data block", .{});
        return -1000;
    }
    if (user_data_block_size % user_count != 0) {
        consoleLog("User data block size not divisible by user count", .{});
        return -1000;
    }
    if (mixnet_size_len == 0) {
        consoleLog("Invalid mixnet size data", .{});
        return -1000;
    }
    if (mixnet_size_len % 4 != 0) {
        consoleLog("Mixnet size length not divisible by 4", .{});
        return -1000;
    }
    if (mixnet_data_block_size == 0) {
        consoleLog("Invalid mixnet data block", .{});
        return -1000;
    }
    if (max_size == 0 or max_size > 1024 * 1024) {
        consoleLog("Invalid max size", .{});
        return -1000;
    }

    // Create read-only slices without taking ownership
    const user_data_block = user_data_block_ptr[0..user_data_block_size];
    const mixnet_data_block = mixnet_data_block_ptr[0..mixnet_data_block_size];
    const mixnet_size_list = mixnet_size_ptr[0..mixnet_size_len];

    const mixnet_data_list = convert_mixnet_data(mixnet_size_list, mixnet_data_block) catch return -1000;
    defer allocator.free(mixnet_data_list);

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
        consoleLog("Error validate: {}", .{err});
        return -1000;
    };

    return result;
}

fn convert_mixnet_data(
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

    var u32_slice = ManagedBuffer.init(mixnet_count * @sizeOf(u32)) catch return error.OutOfMemory;
    defer u32_slice.deinit();
    const u32_data = std.mem.bytesAsSlice(u32, u32_slice.data);

    for (0..mixnet_count) |i| {
        u32_data[i] = std.mem.readInt(u32, mixnet_size_list[i * 4 ..][0..4], .little);
    }

    const mixnet_data_list = try allocator.alloc([]const u8, mixnet_count);
    errdefer allocator.free(mixnet_data_list);

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

fn successSizedBuffer(buf: []const u8) ?[*]u8 {
    const result = allocator.alloc(u8, buf.len + 4) catch return null;
    std.mem.writeInt(u32, result[0..4], @intCast(buf.len), .little);
    @memcpy(result[4..], buf);
    return result.ptr;
}
