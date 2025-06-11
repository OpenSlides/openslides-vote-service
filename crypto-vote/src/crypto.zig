const std = @import("std");
const assert = std.debug.assert;
const X25519 = std.crypto.dh.X25519;
const XCurve = X25519.Curve;
const EDCurve = std.crypto.sign.Ed25519.Curve;
const Aes256Gcm = std.crypto.aead.aes_gcm.Aes256Gcm;
const Aes256 = std.crypto.core.aes.Aes256;
const HkdfSha256 = std.crypto.kdf.hkdf.HkdfSha256;
const Ed25519 = std.crypto.sign.Ed25519;
const Sha256 = std.crypto.hash.sha2.Sha256;
const Sha512 = std.crypto.hash.sha2.Sha512;

const InvalidPublicKeyError = error{InvalidPublicKey};
const AuthenticationError = std.crypto.errors.AuthenticationError;
const IdentityElementError = std.crypto.errors.IdentityElementError;
const OutOfMemoryError = std.mem.Allocator.Error;
const InvalidCypherError = error{InvalidCypher};
const WeakPublicKeyError = std.crypto.errors.WeakPublicKeyError;

pub const KeyPairMixnet = struct {
    key_secred: [32]u8,
    key_public: [32]u8,

    pub fn generate() KeyPairMixnet {
        const key = X25519.KeyPair.generate();
        return KeyPairMixnet{
            .key_secred = key.secret_key,
            .key_public = key.public_key,
        };
    }
};

pub const KeyPairTrustee = struct {
    key_secred: [32]u8,
    key_public: [32]u8,

    pub fn generate() KeyPairTrustee {
        var random_seed: [32]u8 = undefined;
        while (true) {
            std.crypto.random.bytes(&random_seed);
            return generateDeterministic(random_seed) catch {
                @branchHint(.unlikely);
                continue;
            };
        }
    }

    fn generateDeterministic(seed: [32]u8) error{InvalidKey}!KeyPairTrustee {
        const scalar = generate_scalar(seed);
        return KeyPairTrustee{
            .key_secred = scalar,
            .key_public = (calc_pk(scalar) catch return error.InvalidKey).toBytes(),
        };
    }

    fn generate_scalar(random_seed: [32]u8) EDCurve.scalar.CompressedScalar {
        var az: [Sha512.digest_length]u8 = undefined;
        var h = Sha512.init(.{});
        h.update(&random_seed);
        h.final(&az);
        return az[0..32].*;
    }

    fn calc_pk(scalar: EDCurve.scalar.CompressedScalar) (IdentityElementError || WeakPublicKeyError)!EDCurve {
        return EDCurve.basePoint.mul(scalar);
    }
};

fn encrypt_symmetric(shared_secred: [32]u8, message: []const u8, buf: []u8) void {
    const key_aes = HkdfSha256.extract(&[_]u8{}, &shared_secred);

    // With uses a static nonce. Since the data_key is only used once, this
    // should be secure.
    // TODO: Confirm this.
    const nonce = blk: {
        var n: [Aes256Gcm.nonce_length]u8 = undefined;
        @memset(&n, 0);
        break :blk n;
    };

    {
        // Use the given buffer for the encrypted message and the authentication tag (MAC)
        const encrypted = buf[0..message.len];
        const tag = buf[message.len..][0..Aes256Gcm.tag_length];
        Aes256Gcm.encrypt(encrypted, tag, message, &[_]u8{}, nonce, key_aes);
    }
}

fn decrypt_symmetric(shared_secred: [32]u8, cypher: []const u8, buf: []u8) AuthenticationError!void {
    const key_aes = HkdfSha256.extract(&[_]u8{}, &shared_secred);
    const nonce = blk: {
        var n: [Aes256Gcm.nonce_length]u8 = undefined;
        @memset(&n, 0);
        break :blk n;
    };

    {
        const c_size = cypher.len - Aes256Gcm.tag_length;
        const tag = cypher[c_size..][0..Aes256Gcm.tag_length].*;
        try Aes256Gcm.decrypt(buf[0..c_size], cypher[0..c_size], tag, &[_]u8{}, nonce, key_aes);
    }
}

fn encrypt_bufsize(message_len: usize) usize {
    return X25519.public_length + message_len + Aes256Gcm.tag_length;
}

fn encrypt_x25519(
    key_public: [32]u8,
    message: []const u8,
    seed: [32]u8,
    buf: []u8,
) IdentityElementError![]u8 {
    const encrypted_size = encrypt_bufsize(message.len);
    assert(encrypted_size <= buf.len);

    const key_ephemeral = try X25519.KeyPair.generateDeterministic(seed);
    const shared_secred = try X25519.scalarmult(key_ephemeral.secret_key, key_public);

    encrypt_symmetric(shared_secred, message, buf[32..]);
    // Write the public ephemeral key at the end. This is important, when buf and message are the same.
    buf[0..X25519.public_length].* = key_ephemeral.public_key;
    return buf[0..encrypted_size];
}

fn decrypted_bufsize(cypher_len: usize) usize {
    return cypher_len - X25519.public_length - Aes256Gcm.tag_length;
}

fn decrypt_x25519(
    key_secred: [32]u8,
    cypher: []const u8,
    buf: []u8,
) (IdentityElementError || AuthenticationError)![]u8 {
    const decrypted_size = decrypted_bufsize(cypher.len);
    assert(buf.len >= decrypted_size);

    const key_ephemeral_public = cypher[0..X25519.public_length].*;
    const encrypted = cypher[X25519.public_length..];

    const shared_secred = try X25519.scalarmult(key_secred, key_ephemeral_public);
    try decrypt_symmetric(shared_secred, encrypted, buf);

    return buf[0..decrypted_size];
}

test "x25519 encrypt and decrypt" {
    const key = KeyPairMixnet.generate();
    const msg = "my message to be encrypted";
    const seed = std.mem.zeroes([32]u8);

    var buf_encrypt: [encrypt_bufsize(msg.len)]u8 = undefined;
    const encrypted_message = try encrypt_x25519(key.key_public, msg, seed, &buf_encrypt);

    var buf_decrypt: [msg.len]u8 = undefined;
    const decrypted = try decrypt_x25519(key.key_secred, encrypted_message, &buf_decrypt);
    try std.testing.expectEqualDeep(msg, decrypted);
}

fn combine_public_keys(
    key_public_list: []const [32]u8,
) InvalidPublicKeyError!EDCurve {
    assert(key_public_list.len > 0);

    var combined = EDCurve.fromBytes(key_public_list[0]) catch return error.InvalidPublicKey;
    for (key_public_list[1..]) |other| {
        const other_decoded = EDCurve.fromBytes(other) catch return error.InvalidPublicKey;
        combined = combined.add(other_decoded);
    }
    return combined;
}

fn combine_key_secred(
    key_secred_list: []const EDCurve.scalar.CompressedScalar,
) [32]u8 {
    assert(key_secred_list.len > 0);

    var combined = key_secred_list[0];
    for (key_secred_list[1..]) |other| {
        combined = EDCurve.scalar.add(combined, other);
    }
    return combined;
}

fn encrypt_ed25519(
    key_public_list: []const [32]u8,
    message: []const u8,
    seed: [32]u8,
    buf: []u8,
) (InvalidPublicKeyError || IdentityElementError || WeakPublicKeyError)![]u8 {
    const encrypted_size = encrypt_bufsize(message.len);
    assert(encrypted_size <= buf.len);
    assert(key_public_list.len > 0);

    const combined_key_public = combine_public_keys(key_public_list) catch return error.InvalidPublicKey;

    const key_ephemeral = try Ed25519.KeyPair.generateDeterministic(seed);
    const key_ephemeral_secred = extract_scalar(key_ephemeral);
    const key_ephemeral_public = EDCurve.fromBytes(key_ephemeral.public_key.toBytes()) catch unreachable;
    const public_key_bytes = key_ephemeral_public.toBytes();

    const shared_secred = (try EDCurve.mul(combined_key_public, key_ephemeral_secred)).toBytes();
    encrypt_symmetric(shared_secred, message, buf[32..]);
    // Write the public ephemeral key at the end. This is important, when buf and message are the same.
    buf[0..32].* = public_key_bytes;
    return buf[0..encrypted_size];
}

fn extract_scalar(kp: Ed25519.KeyPair) [32]u8 {
    var az: [Sha512.digest_length]u8 = undefined;
    var h = Sha512.init(.{});
    h.update(&kp.secret_key.seed());
    h.final(&az);
    EDCurve.scalar.clamp(az[0..32]);
    return az[0..32].*;
}

fn decrypt_ed25519(
    key_secred_list: []const EDCurve.scalar.CompressedScalar,
    cypher: []const u8,
    buf: []u8,
) (InvalidCypherError || IdentityElementError || WeakPublicKeyError || AuthenticationError)![]u8 {
    const decrypted_size = decrypted_bufsize(cypher.len);
    assert(buf.len >= decrypted_size);
    assert(key_secred_list.len > 0);

    const combined_key_secred = combine_key_secred(key_secred_list);

    const key_ephemeral_public = EDCurve.fromBytes(cypher[0..32].*) catch return error.InvalidCypher;
    const encrypted = cypher[32..];

    const shared_secred = (try EDCurve.mul(key_ephemeral_public, combined_key_secred)).toBytes();
    try decrypt_symmetric(shared_secred, encrypted, buf);
    return buf[0..decrypted_size];
}

test "encrypt and decrypt with ed25519" {
    const key1 = KeyPairTrustee.generate();
    const key2 = KeyPairTrustee.generate();
    const key3 = KeyPairTrustee.generate();
    const msg = "my message to be encrypted";
    const seed = std.mem.zeroes([32]u8);

    const key_public_list = &[_][32]u8{
        key1.key_public,
        key2.key_public,
        key3.key_public,
    };

    var buf_encrypt: [encrypt_bufsize(msg.len)]u8 = undefined;
    const encrypted_message = try encrypt_ed25519(
        key_public_list,
        msg,
        seed,
        &buf_encrypt,
    );

    const key_secred_list = &[_]EDCurve.scalar.CompressedScalar{
        key1.key_secred,
        key2.key_secred,
        key3.key_secred,
    };

    var buf_decrypt: [msg.len]u8 = undefined;
    const decrypted = try decrypt_ed25519(
        key_secred_list,
        encrypted_message,
        &buf_decrypt,
    );
    try std.testing.expectEqualDeep(msg, decrypted);
}

pub fn encrypt_full_buf_size(message_size: usize, mixnet_count: usize, trustee_count: usize) usize {
    return 2 * (message_size + (32 + 16) * (mixnet_count + trustee_count));
}

pub fn encrypt_full(
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    message: []const u8,
    seed: []const u8,
    buf: []u8,
) (InvalidPublicKeyError || IdentityElementError || WeakPublicKeyError)![]u8 {
    const full = encrypt_full_buf_size(message.len, mixnet_key_public_list.len, trustee_key_public_list.len);
    const buffer_mid = full / 2;
    assert(buf.len >= full);
    assert(seed.len == (mixnet_key_public_list.len + 1) * 32);

    var cypher = try encrypt_ed25519(trustee_key_public_list, message, seed[0..32].*, buf[buffer_mid..]);
    @memcpy(buf[0..cypher.len], buf[buffer_mid..][0..cypher.len]);
    cypher = buf[0..cypher.len];

    var i = mixnet_key_public_list.len;
    while (i > 0) {
        const mixnet_seed = seed[i * 32 ..][0..32];
        i -= 1;
        const key_public = mixnet_key_public_list[i];
        cypher = try encrypt_x25519(key_public, cypher, mixnet_seed.*, buf[buffer_mid..]);
        @memcpy(buf[0..cypher.len], buf[buffer_mid..][0..cypher.len]);
        cypher = buf[0..cypher.len];
    }

    return cypher;
}

test "encrypt_full" {
    const trustee_key1 = KeyPairTrustee.generate();
    const trustee_key2 = KeyPairTrustee.generate();
    const trustee_key3 = KeyPairTrustee.generate();
    const mixnet_key1 = KeyPairMixnet.generate();
    const mixnet_key2 = KeyPairMixnet.generate();
    const mixnet_key3 = KeyPairMixnet.generate();
    const msg = "my message to be encrypted";
    const seed = std.mem.zeroes([128]u8);

    const trustee_sk_list = &[_][32]u8{
        trustee_key1.key_secred,
        trustee_key2.key_secred,
        trustee_key3.key_secred,
    };

    const trustee_pk_list = &[_][32]u8{
        trustee_key1.key_public,
        trustee_key2.key_public,
        trustee_key3.key_public,
    };

    const mixnet_pk_list = &[_][32]u8{
        mixnet_key1.key_public,
        mixnet_key2.key_public,
        mixnet_key3.key_public,
    };

    var buf: [encrypt_full_buf_size(msg.len, mixnet_pk_list.len, trustee_pk_list.len)]u8 = undefined;
    var cypher = try encrypt_full(mixnet_pk_list, trustee_pk_list, msg, &seed, &buf);

    var decrypt_buf: [1024]u8 = undefined;
    cypher = try decrypt_x25519(mixnet_key1.key_secred, cypher, &decrypt_buf);
    cypher = try decrypt_x25519(mixnet_key2.key_secred, cypher, &decrypt_buf);
    cypher = try decrypt_x25519(mixnet_key3.key_secred, cypher, &decrypt_buf);

    const decrypted = try decrypt_ed25519(trustee_sk_list, cypher, &decrypt_buf);

    try std.testing.expectEqualDeep(msg, decrypted);
}

pub fn encrypt_message(
    allocator: std.mem.Allocator,
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    message: []const u8,
    size: usize,
) (OutOfMemoryError || InvalidPublicKeyError || WeakPublicKeyError)![]u8 {
    const seed = try allocator.alloc(u8, (mixnet_key_public_list.len + 1) * 32);
    defer allocator.free(seed);

    while (true) {
        std.crypto.random.bytes(seed);
        const cypher = encrypt_message_deterministric(
            allocator,
            mixnet_key_public_list,
            trustee_key_public_list,
            message,
            size,
            seed,
        ) catch |err| {
            @branchHint(.unlikely);
            switch (err) {
                IdentityElementError.IdentityElement => continue,
                else => |leftover| return leftover,
            }
        };

        return cypher;
    }
}

pub fn encrypt_message_deterministric(
    allocator: std.mem.Allocator,
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    message: []const u8,
    size: usize,
    seed: []u8,
) (OutOfMemoryError || InvalidPublicKeyError || IdentityElementError || WeakPublicKeyError)![]u8 {
    const message_with_padding = try allocator.alloc(u8, size);
    defer allocator.free(message_with_padding);
    @memset(message_with_padding, 0);
    @memcpy(message_with_padding, message);

    const buf = try allocator.alloc(u8, encrypt_full_buf_size(
        size,
        mixnet_key_public_list.len,
        trustee_key_public_list.len,
    ));
    errdefer allocator.free(buf);

    var cypher = try encrypt_full(
        mixnet_key_public_list,
        trustee_key_public_list,
        message_with_padding,
        seed,
        buf,
    );

    if (!allocator.resize(buf, cypher.len)) {
        const new_buf = try allocator.alloc(u8, cypher.len);
        @memcpy(new_buf[0..cypher.len], cypher);
        allocator.free(buf);
        cypher = new_buf;
    }

    return cypher;
}

pub fn decrypt_mixnet_buf_size(cypher_block_size: usize, cypher_count: usize) usize {
    assert(cypher_count > 0);
    const cypher_size = cypher_block_size / cypher_count;
    return decrypted_bufsize(cypher_size) * cypher_count;
}

pub fn decrypt_mixnet(
    key_secred: [32]u8,
    cypher_count: usize,
    cypher_block: []const u8,
    buf: []u8,
) (IdentityElementError || AuthenticationError)![]u8 {
    assert(cypher_count > 0);
    assert(cypher_block.len > 0);
    assert(cypher_block.len % cypher_count == 0);
    const cypher_size = cypher_block.len / cypher_count;
    assert(buf.len >= decrypt_mixnet_buf_size(cypher_size, cypher_count));

    const decrypted_size = decrypted_bufsize(cypher_size);

    for (0..cypher_count) |i| {
        const cypher = cypher_block[i * cypher_size ..][0..cypher_size];
        _ = try decrypt_x25519(key_secred, cypher, buf[i * decrypted_size ..]);
    }

    // TODO: Mix messages Each message has to be binary ordered. For that, the
    // buf has to be big enough to fit one other message. Then the messages can
    // be copied to get in the right order. Maybe it is possible to write them
    // directly at there place. If a memory swap is possible, then buf does not
    // need to be bigger.
    return buf[0 .. cypher_count * decrypted_size];
}

pub fn decrypt_trustee_buf_size(cypher_block_size: usize, cypher_count: usize) usize {
    assert(cypher_count > 0);
    const cypher_size = cypher_block_size / cypher_count;
    return decrypted_bufsize(cypher_size) * cypher_count;
}

pub fn decrypt_trustee(
    key_secred_list: []const EDCurve.scalar.CompressedScalar,
    cypher_count: usize,
    cypher_block: []const u8,
    buf: []u8,
) (InvalidCypherError || IdentityElementError || WeakPublicKeyError || AuthenticationError)![]u8 {
    assert(cypher_count > 0);
    assert(cypher_block.len > 0);
    assert(cypher_block.len % cypher_count == 0);
    const cypher_size = cypher_block.len / cypher_count;
    assert(buf.len >= decrypt_trustee_buf_size(cypher_block.len, cypher_count));

    const decrypted_size = decrypted_bufsize(cypher_size);

    for (0..cypher_count) |i| {
        const cypher = cypher_block[i * cypher_size ..][0..cypher_size];
        _ = try decrypt_ed25519(key_secred_list, cypher, buf[i * decrypted_size ..]);
    }

    // TODO: Maybe return individual messages, since this is the cleartext. This
    // would either require the function to request an allocator or a list of
    // buffers (one for each message).
    return buf[0 .. cypher_count * decrypted_size];
}

test "decrypt many messages" {
    const trustee_key1 = KeyPairTrustee.generate();
    const trustee_key2 = KeyPairTrustee.generate();
    const trustee_key3 = KeyPairTrustee.generate();
    const mixnet_key1 = KeyPairMixnet.generate();
    const mixnet_key2 = KeyPairMixnet.generate();
    const mixnet_key3 = KeyPairMixnet.generate();
    const msg1 = "message1";
    const msg2 = "message2";
    const msg_count = 2;
    const seed1 = [_]u8{0} ** 128;
    const seed2 = [_]u8{1} ** 128;
    assert(msg1.len == msg2.len); // All messages need to have the same len

    const trustee_sk_list = &[_][32]u8{
        trustee_key1.key_secred,
        trustee_key2.key_secred,
        trustee_key3.key_secred,
    };

    const trustee_pk_list = &[_][32]u8{
        trustee_key1.key_public,
        trustee_key2.key_public,
        trustee_key3.key_public,
    };

    const mixnet_pk_list = &[_][32]u8{
        mixnet_key1.key_public,
        mixnet_key2.key_public,
        mixnet_key3.key_public,
    };

    const cypher_size = comptime encrypt_full_buf_size(msg1.len, mixnet_pk_list.len, trustee_pk_list.len);
    var buf_cypher1: [cypher_size]u8 = undefined;
    var buf_cypher2: [cypher_size]u8 = undefined;
    const cypher1 = try encrypt_full(mixnet_pk_list, trustee_pk_list, msg1, &seed1, &buf_cypher1);
    const cypher2 = try encrypt_full(mixnet_pk_list, trustee_pk_list, msg2, &seed2, &buf_cypher2);

    // cypher1.len is probably pyher_size/2, so it would be enough to use
    // cypher_size here. But to be sure and safe for future updates, we take
    // cypher_size*2. cypher1.len is not a comptime value and can not be used.
    var cypher_block: [cypher_size * 2]u8 = undefined;
    @memcpy(cypher_block[0..cypher1.len], cypher1);
    @memcpy(cypher_block[cypher1.len..][0..cypher2.len], cypher2);

    var buf_decrypt1: [1024]u8 = undefined;
    const decrypted_from_mixnet1 = try decrypt_mixnet(
        mixnet_key1.key_secred,
        msg_count,
        cypher_block[0 .. cypher1.len * 2],
        &buf_decrypt1,
    );

    var buf_decrypt2: [1024]u8 = undefined;
    const decrypted_from_mixnet2 = try decrypt_mixnet(
        mixnet_key2.key_secred,
        msg_count,
        decrypted_from_mixnet1,
        &buf_decrypt2,
    );

    var buf_decrypt3: [1024]u8 = undefined;
    const decrypted_from_mixnet3 = try decrypt_mixnet(
        mixnet_key3.key_secred,
        msg_count,
        decrypted_from_mixnet2,
        &buf_decrypt3,
    );

    var buf_decrypt4: [1024]u8 = undefined;
    const decryptd_from_trustees = try decrypt_trustee(
        trustee_sk_list,
        msg_count,
        decrypted_from_mixnet3,
        &buf_decrypt4,
    );

    const decrypted1 = decryptd_from_trustees[0..msg1.len];
    const decrypted2 = decryptd_from_trustees[msg1.len..][0..msg1.len];

    try std.testing.expectEqualDeep(msg1, decrypted1);
    try std.testing.expectEqualDeep(msg2, decrypted2);
}
