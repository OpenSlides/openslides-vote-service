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
    Env.console_log(msg.ptr, msg.len);
}

pub fn getRandom(buf: []u8) void {
    Env.get_random(buf.ptr, buf.len);
}

pub const std_options = std.Options{
    .cryptoRandomSeed = getRandom,
};

// gen_mixnet_key_pair creates a keypair for a mixnet member.
//
// On error, the funciton returns 0.
//
// On success, the function returns a pointer to memory, where the keypair is.
// The first 32 byte of the keypair is the secred key. The second 32 byte is the
// public key.
//
// The memory has to be deallocated from the caller.
export fn gen_mixnet_key_pair() ?[*]const u8 {
    const kp = crypto.KeyPairMixnet.generate();

    const result = allocator.alloc(u8, 64) catch return null;
    @memcpy(result[0..32], &kp.key_secred);
    @memcpy(result[32..][0..32], &kp.key_public);
    return result.ptr;
}

// gen_trustee_key_pair creates a keypair for a trustee member.
//
// On error, the funciton returns 0.
//
// On success, the function returns a pointer to memory, where the keypair is.
// The first 32 byte of the keypair is the secred key. The second 32 byte is the
// public key.
//
// The memory has to be deallocated from the caller.
export fn gen_trustee_key_pair() ?[*]const u8 {
    const kp = crypto.KeyPairTrustee.generate();

    const result = allocator.alloc(u8, 64) catch return null;
    @memcpy(result[0..32], &kp.key_secred);
    @memcpy(result[32..][0..32], &kp.key_public);
    return result.ptr;
}

// encrypt encryptes a message for the mixnet and trustees.
//
// First, the message gets encrypted for all trustees at once. Then the result
// gets encrypted for each member of the mixnet, once at a time. It starts with
// the last member of the mixnet, end ends with the first. So the final result
// is a message, that is encrypted many times. To decrypt it, it first has to be
// decryted from mixnet1, then mixnet2 and so on. The value, that was decrypted
// from the last member of the mixnet has to be decrypted with the private keys
// from all trustees.
//
// The argument "mixnet_count" is the amount of members of the mixnet. The
// argument "trustee_count" the amount of members of the trustree group.
//
// The argument mixnet_key_public_ptr has to be a pointer, that points to a list
// of all mixnet keys. The argument trustee_key_public_ptr is the same for the
// trustee group.
//
// msg_ptr has to be a pointer to the message, that has to be encrypted. msg_len
// is the len of this message.
//
// When calling the function, the memory of the mixnet- and trustee public keys
// gets deallocated. Also the memory of the message.
//
// On error, the function returns 0.
//
// On success, the function returns a pointer to memory. The first four bytes of
// this memory is the size following memory (the four bytes are not included).
// The following memory is the encrypted message.
export fn encrypt(
    mixnet_count: u32,
    trustee_count: u32,
    mixnet_key_public_ptr: [*]const [32]u8,
    trustee_key_public_ptr: [*]const [32]u8,
    msg_ptr: [*]const u8,
    msg_len: u32,
) ?[*]u8 {
    const message = msg_ptr[0..msg_len];
    defer allocator.free(message);

    const mixnet_key_public_list: []const [32]u8 = mixnet_key_public_ptr[0..mixnet_count];
    defer allocator.free(mixnet_key_public_list);
    const trustee_key_public_list: []const [32]u8 = trustee_key_public_ptr[0..trustee_count];
    defer allocator.free(trustee_key_public_list);

    const buf = allocator.alloc(u8, crypto.encrypt_full_buf_size(message.len, mixnet_count, trustee_count)) catch return null;
    defer allocator.free(buf);
    const cypher = crypto.encrypt_full(mixnet_key_public_list, trustee_key_public_list, message, buf) catch return null;

    return successSizedBuffer(cypher);
}

// decrypt_mixnet decrypts a block of cyphers with a private key from a mixnet
// member.
//
// For the first member of the mixnet, the input cypher has to be combined
// result for each message encrpted with `encrypt` without the site-prefix.
//
// For each other member of the mixnet, the input cypher is the output of
// `decrypt_mixnet` from the previous mixnet member.
//
// The arugment `key_secred` is the private key of the mixnet member.
//
// The argument `cypher_count` is the amount of messages, that should be
// decrypted.
//
// The argument `cypher_block_ptr` is a pointer to the memory, where the cypher
// block an be found. The argument `cypher_block_size` is the size of the block.
//
// Returns 0 on error.
//
// On success, it returns a pointer to the decrypted data block, with a four
// byte prefix of the new size.
//
// The function call deallocates the secred key and the cypher_block. The caller
// is responsible to deallocate the returnd data.
export fn decrypt_mixnet(
    key_secred: *[32]u8,
    cypher_count: u32,
    cypher_block_ptr: [*]const u8,
    cypher_block_size: u32,
) ?[*]u8 {
    defer allocator.free(key_secred);
    defer allocator.free(cypher_block_ptr[0..cypher_block_size]);

    const buf_size = crypto.decrypt_mixnet_buf_size(cypher_block_size, cypher_count);
    const buf = allocator.alloc(u8, buf_size) catch return null;
    defer allocator.free(buf);

    const decrypted = crypto.decrypt_mixnet(
        key_secred.*,
        cypher_count,
        cypher_block_ptr[0..cypher_block_size],
        buf,
    ) catch return null;
    return successSizedBuffer(decrypted);
}

// decrypt_trustee decrypts a block of cyphers with all keys from the trustees.
//
// The input has to be the cypher block returned from `decrypt_mixnet` from the
// last member of the mixnet.
//
// The argument `trustee_count` is the amount of trustees. The arugment
// `key_secred_list` is a pointer to all secred keys from each trustee.
//
// The argument `cypher_count` is the amount of messages, that should be
// decrypted.
//
// The argument `cypher_block_ptr` is a pointer to the memory, where the cypher
// block an be found. The argument `cypher_block_size` is the size of the block.
//
// Returns 0 on error.
//
// On success, it returns a pointer to the decrypted data block, with a four
// byte prefix of the new size. It can be devided in chunks of `cypher_count`
// blocks to get the list of decrypted messages.
//
// The function call deallocates the list of secred keys and the cypher_block.
// The caller is responsible to deallocate the returnd data.
export fn decrypt_trustee(
    trustee_count: u32,
    key_secred_list: [*]const [32]u8,
    cypher_count: u32,
    cypher_block_ptr: [*]const u8,
    cypher_block_size: u32,
) ?[*]u8 {
    consoleLog("cypher_block_size: {}, cypher_count: {}", .{ cypher_block_size, cypher_count });
    defer allocator.free(key_secred_list[0..trustee_count]);
    defer allocator.free(cypher_block_ptr[0..cypher_block_size]);

    const buf_size = crypto.decrypt_trustee_buf_size(cypher_block_size, cypher_count);

    const buf = allocator.alloc(u8, buf_size) catch |err| {
        consoleLog("Error allocating {} bytes of memory for decrypt buf: {}", .{ buf_size, err });
        return null;
    };
    defer allocator.free(buf);

    consoleLog("Hello World3", .{});

    const decrypted = crypto.decrypt_trustee(
        key_secred_list[0..trustee_count],
        cypher_count,
        cypher_block_ptr[0..cypher_block_size],
        buf,
    ) catch |err| {
        consoleLog("Error calling decrypt_trustee: {}", .{err});
        return null;
    };
    return successSizedBuffer(decrypted);
}

fn successSizedBuffer(buf: []const u8) ?[*]u8 {
    const result = allocator.alloc(u8, buf.len + 4) catch return null;
    std.mem.writeInt(u32, result[0..4], @intCast(buf.len), .little);
    @memcpy(result[4..], buf);
    return result.ptr;
}
