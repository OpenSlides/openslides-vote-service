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

/// Key pair for mixnet nodes using X25519 cryptography.
/// Mixnet nodes are responsible for shuffling
/// to provide anonymity in the voting system.
pub const KeyPairMixnet = struct {
    /// Secret key used for decryption (32 bytes)
    key_secret: [32]u8,
    /// Public key used for encryption (32 bytes)
    key_public: [32]u8,

    /// Generates a new random key pair for a mixnet node.
    ///
    /// Returns:
    ///   KeyPairMixnet: A new key pair with cryptographically secure random keys
    pub fn generate() KeyPairMixnet {
        const key = X25519.KeyPair.generate();
        return KeyPairMixnet{
            .key_secret = key.secret_key,
            .key_public = key.public_key,
        };
    }
};

/// Key pair for trustee nodes.
///
/// This keypair uses the Edwards-Curve instead of the Montgomery form. The
/// reason is, that x25519 only uses the X-Coordinate of the curve. But for our
/// key-combination algorithm, we need the exact point.
pub const KeyPairTrustee = struct {
    /// Secret key used for decryption.
    key_secret: [32]u8,
    /// Public key used for encryption.
    key_public: [32]u8,

    /// Generates a new random key pair for a trustee.
    ///
    /// Returns:
    ///   KeyPairTrustee: A new key pair with cryptographically secure random keys
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

    /// Generates a deterministic key pair from a given seed.
    ///
    /// Args:
    ///   seed: 32-byte random seed for key generation
    ///
    /// Returns:
    ///   KeyPairTrustee: Generated key pair
    ///   error.IdentityElementError or WeakPublicKeyError: If the seed produces an invalid key (very rare)
    fn generateDeterministic(seed: [32]u8) (IdentityElementError || WeakPublicKeyError)!KeyPairTrustee {
        return KeyPairTrustee{
            .key_secret = seed,
            .key_public = try recoverPublicKey(seed),
        };
    }

    /// Calculates the public key for a secred key.
    ///
    /// The same as x25519.recoverPublicKey, but uses the Edwards curve.
    fn recoverPublicKey(secret_key: [32]u8) (IdentityElementError || WeakPublicKeyError)![32]u8 {
        const q = try EDCurve.basePoint.clampedMul(secret_key);
        return q.toBytes();
    }
};

/// Encrypts a message using AES-256-GCM with a shared secret.
/// Uses a zero nonce which is safe because each shared secret is used only once.
///
/// Args:
///   shared_secret: 32-byte shared secret derived from key exchange
///   message: Plaintext message to encrypt
///   buf: Output buffer (must be at least message.len + 16 bytes for tag)
fn encrypt_symmetric(shared_secret: [32]u8, message: []const u8, buf: []u8) void {
    assert(buf.len >= message.len + 16);

    const key_aes = HkdfSha256.extract(&[_]u8{}, &shared_secret);

    // With uses a static nonce. Since the data_key is only used once, this is
    // secure.
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

/// Decrypts a message using AES-256-GCM with a shared secret.
///
/// Args:
///   shared_secret: 32-byte shared secret used for encryption
///   cypher: Encrypted message including authentication tag
///   buf: Output buffer for decrypted message
///
/// Returns:
///   AuthenticationError: If authentication tag verification fails
fn decrypt_symmetric(shared_secret: [32]u8, cypher: []const u8, buf: []u8) AuthenticationError!void {
    assert(buf.len >= cypher.len - 16);

    const key_aes = HkdfSha256.extract(&[_]u8{}, &shared_secret);
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

/// Calculates the required buffer size for encryption output.
///
/// Args:
///   message_len: Length of the message to encrypt
///
/// Returns:
///   usize: Required buffer size (32 bytes ephemeral key + message + 16 bytes tag)
fn encrypt_bufsize(message_len: usize) usize {
    return X25519.public_length + message_len + Aes256Gcm.tag_length;
}

/// Encrypts a message using X25519 ECIES with deterministic ephemeral key.
///
/// Args:
///   key_public: Recipient's X25519 public key
///   message: Plaintext message to encrypt
///   seed: 32-byte seed for deterministic ephemeral key generation
///   buf: Output buffer for encrypted data
///
/// Returns:
///   []u8: Encrypted data (ephemeral_public_key || encrypted_message || tag)
///   IdentityElementError: If key exchange results in identity element
fn encrypt_x25519_deterministic(
    key_public: [32]u8,
    message: []const u8,
    seed: [32]u8,
    buf: []u8,
) IdentityElementError![]u8 {
    const encrypted_size = encrypt_bufsize(message.len);
    assert(encrypted_size <= buf.len);

    const key_ephemeral = try X25519.KeyPair.generateDeterministic(seed);
    const shared_secret = try X25519.scalarmult(key_ephemeral.secret_key, key_public);

    encrypt_symmetric(shared_secret, message, buf[32..]);
    // Write the public ephemeral key at the end. This is important, when buf and message are the same.
    buf[0..X25519.public_length].* = key_ephemeral.public_key;
    return buf[0..encrypted_size];
}

/// Encrypts a message with x25519 using a random ephemeral key.
///
/// Args:
///   key_public_list: List of Ed25519 public keys to encrypt for
///   message: Plaintext message to encrypt
///   buf: Output buffer for encrypted data
///
/// Returns:
///   []u8: Encrypted data (ephemeral_public_key || encrypted_message || tag)
///   InvalidPublicKeyError: If any public key is invalid
///   WeakPublicKeyError: If ephemeral key generation produces weak key
fn encrypt_x25519(
    key_public: [32]u8,
    message: []const u8,
    buf: []u8,
) (InvalidPublicKeyError || WeakPublicKeyError)![]u8 {
    var random_seed: [32]u8 = undefined;
    while (true) {
        std.crypto.random.bytes(&random_seed);
        return encrypt_x25519_deterministic(key_public, message, random_seed, buf) catch |err| {
            @branchHint(.unlikely);
            switch (err) {
                IdentityElementError.IdentityElement => continue,
                else => |leftover| return leftover,
            }
        };
    }
}

/// Calculates the size of decrypted message from cypher length.
///
/// Args:
///   cypher_len: Length of the encrypted data
///
/// Returns:
///   usize: Size of the decrypted message
fn decrypted_bufsize(cypher_len: usize) usize {
    return cypher_len - X25519.public_length - Aes256Gcm.tag_length;
}

/// Decrypts a message encrypted with X25519 ECIES.
///
/// Args:
///   key_secret: Recipient's X25519 secret key
///   cypher: Encrypted data (ephemeral_public_key || encrypted_message || tag)
///   buf: Output buffer for decrypted message
///
/// Returns:
///   []u8: Decrypted message
///   IdentityElementError: If key exchange results in identity element
///   AuthenticationError: If authentication tag verification fails
fn decrypt_x25519(
    key_secret: [32]u8,
    cypher: []const u8,
    buf: []u8,
) (IdentityElementError || AuthenticationError)![]u8 {
    const decrypted_size = decrypted_bufsize(cypher.len);
    assert(buf.len >= decrypted_size);

    const key_ephemeral_public = cypher[0..X25519.public_length].*;
    const encrypted = cypher[X25519.public_length..];

    const shared_secret = try X25519.scalarmult(key_secret, key_ephemeral_public);
    try decrypt_symmetric(shared_secret, encrypted, buf);

    return buf[0..decrypted_size];
}

/// Like decrypt_x25519 but without clamping the secret key.
///
/// The combined trustee key can not be clamped. When decrypting a message
/// encrypted with the combined trustee key, it can not be clamped.
fn decrypt_x25519_no_clamp(
    key_secret: [32]u8,
    cypher: []const u8,
    buf: []u8,
) (IdentityElementError || AuthenticationError || WeakPublicKeyError)![]u8 {
    const decrypted_size = decrypted_bufsize(cypher.len);
    assert(buf.len >= decrypted_size);

    const key_ephemeral_public = cypher[0..X25519.public_length].*;
    const encrypted = cypher[X25519.public_length..];

    const shared_secret = try scalarmultNoClap(key_secret, key_ephemeral_public);
    try decrypt_symmetric(shared_secret, encrypted, buf);

    return buf[0..decrypted_size];
}

fn scalarmultNoClap(secret_key: [32]u8, public_key: [32]u8) ![32]u8 {
    const q = try XCurve.fromBytes(public_key).mul(secret_key);
    return q.toBytes();
}

test "x25519 encrypt and decrypt" {
    const key = KeyPairMixnet.generate();
    const msg = "my message to be encrypted";
    const seed = std.mem.zeroes([32]u8);

    var buf_encrypt: [encrypt_bufsize(msg.len)]u8 = undefined;
    const encrypted_message = try encrypt_x25519_deterministic(key.key_public, msg, seed, &buf_encrypt);

    var buf_decrypt: [msg.len]u8 = undefined;
    const decrypted = try decrypt_x25519(key.key_secret, encrypted_message, &buf_decrypt);
    try std.testing.expectEqualDeep(msg, decrypted);
}

/// Combines multiple trustee public keys into a single aggregated key.
///
/// Normaly, a public key is clamped. This is not possible here. A public key
/// can not be clamped direcly. The secred key has to be clamped before
/// calculating the secred key. But since all public keys where clamped before
/// publishing them, the aggregated key does not need clamping.
///
/// Clamping has to reasons. The lowest bits are set so zero to get a multible
/// of 8. This is important to get a point on the correct subgroup of the curve.
/// But since all points are on the secure curve, the addition of all points
/// still is a point on the secure subgroup.
///
/// The other reason for claming is to get a key, that is not vulnerable to
/// timing attacs. Since the secure trustee key is known publicly, this a timing
/// attack is useless here.
///
/// Args:
///   key_public_list: Array of trustee public keys to combine.
///
/// Returns:
///   x25519 public key: Combined public key point converted for x25519.
///   InvalidPublicKeyError: If any public key is invalid.
fn combine_public_keys_to_x25519(
    key_public_list: []const [32]u8,
) InvalidPublicKeyError![32]u8 {
    assert(key_public_list.len > 0);

    var combined = EDCurve.fromBytes(key_public_list[0]) catch return error.InvalidPublicKey;
    for (key_public_list[1..]) |other| {
        const other_decoded = EDCurve.fromBytes(other) catch return error.InvalidPublicKey;
        combined = combined.add(other_decoded);
    }

    const key_public_x25519 = XCurve.fromEdwards25519(combined) catch return error.InvalidPublicKey;
    return key_public_x25519.toBytes();
}

/// Combines multiple trustee secret keys into a single aggregated key.
///
/// This key can be used for x25519, but the result is not clamped. Therefore
/// the function decrypt_x25519_no_clamp has to be used for decryption.
///
/// Args:
///   key_secret_list: Array of Ed25519 secret scalars to combine
///
/// Returns:
///   [32]u8: Combined secret scalar
fn combine_key_secret(
    key_secret_list: []const EDCurve.scalar.CompressedScalar,
) [32]u8 {
    assert(key_secret_list.len > 0);

    // When generating the keys, the secred keys where clamped before calculating
    // the public keys. But the secred keys where saved in there unclamped form.
    // Therefore they have to be clamped before adding them together.
    var combined = key_secret_list[0];
    EDCurve.scalar.clamp(&combined);
    for (key_secret_list[1..]) |other| {
        var other_clamped = other;
        EDCurve.scalar.clamp(&other_clamped);
        combined = EDCurve.scalar.add(combined, other_clamped);
    }
    return combined;
}

test "encrypt and decrypt trustee" {
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

    const key_public_x25519 = try combine_public_keys_to_x25519(key_public_list);

    var buf_encrypt: [encrypt_bufsize(msg.len)]u8 = undefined;
    const encrypted_message = try encrypt_x25519_deterministic(
        key_public_x25519,
        msg,
        seed,
        &buf_encrypt,
    );

    const key_secret_list = &[_]EDCurve.scalar.CompressedScalar{
        key1.key_secret,
        key2.key_secret,
        key3.key_secret,
    };

    const key_secret_combined = combine_key_secret(key_secret_list);

    var buf_decrypt: [msg.len]u8 = undefined;
    const decrypted = try decrypt_x25519_no_clamp(
        key_secret_combined,
        encrypted_message,
        &buf_decrypt,
    );
    try std.testing.expectEqualDeep(msg, decrypted);
}

/// Calculates the final encrypted message size after mixnet and trustee encryption.
/// Each encryption layer adds overhead for ephemeral keys and authentication tags.
///
/// Args:
///   message_size: Size of the original message
///   mixnet_count: Number of mixnet nodes in the chain
///
/// Returns:
///   usize: Final encrypted message size
pub fn calc_cypher_size(message_size: usize, mixnet_count: usize) usize {
    return (message_size + (32 + 16) * (mixnet_count + 1));
}

/// Calculates buffer size needed for full encryption (real + fake message).
///
/// Args:
///   message_size: Size of the original message
///   mixnet_count: Number of mixnet nodes
///
/// Returns:
///   usize: Required buffer size for both real and fake cyphers
fn encrypt_full_buf_size(message_size: usize, mixnet_count: usize) usize {
    return 2 * calc_cypher_size(message_size, mixnet_count);
}

/// Encrypts a message through the full mixnet and trustee chain.
/// First encrypts for trustees, then for each mixnet node in reverse order.
///
/// Args:
///   mixnet_key_public_list: Public keys of mixnet nodes
///   trustee_key_public_list: Public keys of trustees
///   message: Message to encrypt
///   seed: Deterministic seeds for encryption
///   buf: Output buffer
///
/// Returns:
///   []u8: Fully encrypted message
///   InvalidPublicKeyError: If any public key is invalid
///   IdentityElementError: If key exchange results in identity element
///   WeakPublicKeyError: If any generated key is weak
fn encrypt_full(
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    message: []const u8,
    seed: []const u8,
    buf: []u8,
) (InvalidPublicKeyError || IdentityElementError || WeakPublicKeyError)![]u8 {
    const full = encrypt_full_buf_size(message.len, mixnet_key_public_list.len);
    const buffer_mid = full / 2;
    assert(buf.len >= full);
    assert(seed.len == (mixnet_key_public_list.len + 1) * 32);

    const trustee_key_public_x25519 = try combine_public_keys_to_x25519(trustee_key_public_list);
    var cypher = try encrypt_x25519_deterministic(
        trustee_key_public_x25519,
        message,
        seed[0..32].*,
        buf[buffer_mid..],
    );
    @memcpy(buf[0..cypher.len], buf[buffer_mid..][0..cypher.len]);
    cypher = buf[0..cypher.len];

    var i = mixnet_key_public_list.len;
    while (i > 0) {
        const mixnet_seed = seed[i * 32 ..][0..32];
        i -= 1;
        const key_public = mixnet_key_public_list[i];
        cypher = try encrypt_x25519_deterministic(key_public, cypher, mixnet_seed.*, buf[buffer_mid..]);
        @memcpy(buf[0..cypher.len], buf[buffer_mid..][0..cypher.len]);
        cypher = buf[0..cypher.len];
    }

    return cypher;
}

/// Generates fake encryption steps for verification purposes.
/// Creates encrypted dummy data at each stage of the mixnet for validation.
///
/// Args:
///   allocator: Memory allocator
///   mixnet_key_public_list: Public keys of mixnet nodes
///   trustee_key_public_list: Public keys of trustees
///   seed: Deterministic seeds for fake encryption
///   message_size: Size of dummy message to encrypt
///
/// Returns:
///   [][]u8: Array of fake encrypted data for each mixnet stage
fn encrypt_fake_steps(
    allocator: std.mem.Allocator,
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    seed: []const u8,
    message_size: usize,
) ![][]u8 {
    const message = try allocator.alloc(u8, message_size);
    defer allocator.free(message);
    @memset(message, 0);

    const result = try allocator.alloc([]u8, mixnet_key_public_list.len + 1);
    errdefer {
        for (result) |v| {
            allocator.free(v);
        }
        allocator.free(result);
    }

    const trustee_key_public_x25519 = try combine_public_keys_to_x25519(trustee_key_public_list);
    var cypher = try allocator.alloc(u8, encrypt_bufsize(message.len));
    _ = try encrypt_x25519_deterministic(
        trustee_key_public_x25519,
        message,
        seed[0..32].*,
        cypher,
    );
    result[result.len - 1] = cypher;

    var cypher_size = cypher.len;

    var i = mixnet_key_public_list.len;
    while (i > 0) {
        const mixnet_seed = seed[i * 32 ..][0..32];
        i -= 1;
        const key_public = mixnet_key_public_list[i];
        const mixnet_cypher = try allocator.alloc(u8, encrypt_bufsize(cypher_size));
        cypher = try encrypt_x25519_deterministic(key_public, cypher, mixnet_seed.*, mixnet_cypher);
        result[i] = cypher;
        cypher_size = cypher.len;
    }

    return result;
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
        trustee_key1.key_secret,
        trustee_key2.key_secret,
        trustee_key3.key_secret,
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

    var buf: [encrypt_full_buf_size(msg.len, mixnet_pk_list.len)]u8 = undefined;
    var cypher = try encrypt_full(mixnet_pk_list, trustee_pk_list, msg, &seed, &buf);

    var decrypt_buf: [1024]u8 = undefined;
    cypher = try decrypt_x25519(mixnet_key1.key_secret, cypher, &decrypt_buf);
    cypher = try decrypt_x25519(mixnet_key2.key_secret, cypher, &decrypt_buf);
    cypher = try decrypt_x25519(mixnet_key3.key_secret, cypher, &decrypt_buf);

    const trustee_key_secret = combine_key_secret(trustee_sk_list);
    const decrypted = try decrypt_x25519_no_clamp(trustee_key_secret, cypher, &decrypt_buf);

    try std.testing.expectEqualDeep(msg, decrypted);
}

/// Container for encrypted data and its corresponding seed.
/// Used to track the randomness used for encryption for later verification.
const CypherSeed = struct {
    /// Encrypted message data
    cypher: []u8,
    /// Seed used for deterministic encryption
    seed: []u8,
};

/// Encrypts a message with padding to a fixed size.
///
/// Args:
///   allocator: Memory allocator
///   mixnet_key_public_list: Public keys of mixnet nodes
///   trustee_key_public_list: Public keys of trustees
///   message: Message to encrypt
///   size: Target size after padding
///
/// Returns:
///   CypherSeed: Encrypted data and seed used
///   OutOfMemoryError: If memory allocation fails
///   InvalidPublicKeyError: If any public key is invalid
///   WeakPublicKeyError: If any generated key is weak
fn encrypt_fixed_size(
    allocator: std.mem.Allocator,
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    message: []const u8,
    size: usize,
) (OutOfMemoryError || InvalidPublicKeyError || WeakPublicKeyError)!CypherSeed {
    const seed = try allocator.alloc(u8, (mixnet_key_public_list.len + 1) * 32);
    errdefer allocator.free(seed);

    while (true) {
        std.crypto.random.bytes(seed);
        const cypher = encrypt_fixed_size_deterministic(
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

        return .{ .cypher = cypher, .seed = seed };
    }
}

/// Encrypts a message with padding to a fixed size using deterministic randomness.
///
/// Args:
///   allocator: Memory allocator
///   mixnet_key_public_list: Public keys of mixnet nodes
///   trustee_key_public_list: Public keys of trustees
///   message: Message to encrypt
///   size: Target size after padding
///   seed: Deterministic seed for encryption
///
/// Returns:
///   []u8: Encrypted message of fixed size
///   OutOfMemoryError: If memory allocation fails
///   InvalidPublicKeyError: If any public key is invalid
///   IdentityElementError: If key exchange results in identity element
///   WeakPublicKeyError: If any generated key is weak
pub fn encrypt_fixed_size_deterministic(
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
    @memcpy(message_with_padding[0..message.len], message);

    const buf = try allocator.alloc(u8, encrypt_full_buf_size(
        size,
        mixnet_key_public_list.len,
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

/// Result of message encryption containing real/fake cyphers and control data.
/// The system generates both a real encrypted message and a fake one to provide
/// deniability - external observers cannot determine which is which.
pub const EncryptResult = struct {
    const Self = @This();
    /// Two encrypted messages: one real, one fake (order is randomized)
    cyphers: [2][]const u8,
    /// Control data containing the seed used for fake message encryption
    control_data: []const u8,

    /// Reconstructs an EncryptResult from serialized bytes.
    ///
    /// Args:
    ///   bytes: Serialized encryption result
    ///   mixnet_count: Number of mixnet nodes (for size calculation)
    ///   max_size: Maximum message size (for size calculation)
    ///
    /// Returns:
    ///   Self: Reconstructed EncryptResult
    pub fn fromBytes(bytes: []const u8, mixnet_count: usize, max_size: usize) Self {
        const cypher_size = calc_cypher_size(max_size, mixnet_count);
        assert(bytes.len == 2 * cypher_size + encrypt_bufsize((mixnet_count + 1) * 32));

        return .{
            .cyphers = [2][]const u8{
                bytes[0..cypher_size],
                bytes[cypher_size..][0..cypher_size],
            },
            .control_data = bytes[cypher_size * 2 ..],
        };
    }

    pub fn toBytes(self: Self, allocator: std.mem.Allocator) ![]u8 {
        assert(self.cyphers[0].len == self.cyphers[1].len);

        const cypher_len = self.cyphers[0].len;
        const size = cypher_len * 2 + self.control_data.len;
        const result = try allocator.alloc(u8, size);
        @memcpy(result[0..cypher_len], self.cyphers[0]);
        @memcpy(result[cypher_len..][0..cypher_len], self.cyphers[1]);
        @memcpy(result[cypher_len * 2 ..][0..self.control_data.len], self.control_data);
        return result;
    }

    /// Frees all allocated memory in this EncryptResult.
    ///
    /// Args:
    ///   allocator: The allocator used to create this result
    pub fn free(self: Self, allocator: std.mem.Allocator) void {
        allocator.free(self.cyphers[0]);
        allocator.free(self.cyphers[1]);
        allocator.free(self.control_data);
    }

    /// Serializes the EncryptResult to bytes with a size prefix.
    ///
    /// Args:
    ///   allocator: Memory allocator for the result
    ///
    /// Returns:
    ///   [*]u8: Pointer to serialized data (size_prefix || cypher1 || cypher2 || control_data)
    pub fn toBytesWithPrefix(self: Self, allocator: std.mem.Allocator) ![*]u8 {
        assert(self.cyphers[0].len == self.cyphers[1].len);

        const cypher_len = self.cyphers[0].len;
        const size = cypher_len * 2 + self.control_data.len;
        const result = try allocator.alloc(u8, size + 4);
        std.mem.writeInt(u32, result[0..4], @intCast(size), .little);
        @memcpy(result[4..][0..cypher_len], self.cyphers[0]);
        @memcpy(result[4 + cypher_len ..][0..cypher_len], self.cyphers[1]);
        @memcpy(result[4 + cypher_len * 2 ..][0..self.control_data.len], self.control_data);
        return result.ptr;
    }
};

/// Encrypts a voting message with anonymity protection.
/// Creates both real and fake encrypted messages to provide deniability.
/// The fake message is encrypted with a random seed that gets encrypted
/// as control data for later verification.
///
/// Args:
///   allocator: Memory allocator
///   mixnet_key_public_list: Public keys of all mixnet nodes
///   trustee_key_public_list: Public keys of all trustees
///   message: The voting message to encrypt
///   size: Target size for padding (must be >= message.len)
///
/// Returns:
///   EncryptResult: Structure containing two cyphers and control data
///     - cyphers[0] and cyphers[1]: One real, one fake (order randomized)
///     - control_data: Encrypted seed for fake message verification
pub fn encrypt_message(
    allocator: std.mem.Allocator,
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    message: []const u8,
    size: usize,
) !EncryptResult {
    const cypher_real = try encrypt_fixed_size(allocator, mixnet_key_public_list, trustee_key_public_list, message, size);
    allocator.free(cypher_real.seed);

    const zeros = try allocator.alloc(u8, size);
    defer allocator.free(zeros);
    @memset(zeros, 0);

    const cypher_fake = try encrypt_fixed_size(allocator, mixnet_key_public_list, trustee_key_public_list, zeros, size);
    defer allocator.free(cypher_fake.seed);

    const trustee_key_public_combined = try combine_public_keys_to_x25519(trustee_key_public_list);
    const buf = try allocator.alloc(u8, encrypt_bufsize(cypher_fake.seed.len));
    const control_data = try encrypt_x25519(trustee_key_public_combined, cypher_fake.seed, buf);

    return if (std.Random.boolean(std.crypto.random))
        .{ .cyphers = [2][]u8{ cypher_real.cypher, cypher_fake.cypher }, .control_data = control_data }
    else
        .{ .cyphers = [2][]u8{ cypher_fake.cypher, cypher_real.cypher }, .control_data = control_data };
}

test "encrypt_message" {
    const allocator = std.testing.allocator;
    const trustee_key1 = KeyPairTrustee.generate();
    const trustee_key2 = KeyPairTrustee.generate();
    const trustee_key3 = KeyPairTrustee.generate();
    const mixnet_key1 = KeyPairMixnet.generate();
    const mixnet_key2 = KeyPairMixnet.generate();
    const mixnet_key3 = KeyPairMixnet.generate();
    const msg = "my message to be encrypted";
    const max_size = msg.len + 10;

    const trustee_sk_list = &[_][32]u8{
        trustee_key1.key_secret,
        trustee_key2.key_secret,
        trustee_key3.key_secret,
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

    const result = try encrypt_message(
        allocator,
        mixnet_pk_list,
        trustee_pk_list,
        msg,
        max_size,
    );
    defer result.free(allocator);

    const cypher_block = try std.mem.concat(allocator, u8, &result.cyphers);
    defer allocator.free(cypher_block);

    const decrypted_from_mixnet1 = try decrypt_mixnet(
        allocator,
        mixnet_key1.key_secret,
        2,
        cypher_block,
    );
    defer allocator.free(decrypted_from_mixnet1);

    const decrypted_from_mixnet2 = try decrypt_mixnet(
        allocator,
        mixnet_key2.key_secret,
        2,
        decrypted_from_mixnet1,
    );
    defer allocator.free(decrypted_from_mixnet2);

    const decrypted_from_mixnet3 = try decrypt_mixnet(
        allocator,
        mixnet_key3.key_secret,
        2,
        decrypted_from_mixnet2,
    );
    defer allocator.free(decrypted_from_mixnet3);

    var buf_decrypt4: [1024]u8 = undefined;
    const decryptd_from_trustees = try decrypt_trustee(
        trustee_sk_list,
        2,
        decrypted_from_mixnet3,
        &buf_decrypt4,
    );

    const decrypted1 = std.mem.trimRight(u8, decryptd_from_trustees[0..max_size], "\x00");
    const decrypted2 = std.mem.trimRight(u8, decryptd_from_trustees[max_size..][0..max_size], "\x00");

    if (decrypted1.len == 0) {
        try std.testing.expectEqualDeep("", decrypted1);
        try std.testing.expectEqualDeep(msg, decrypted2);
    } else if (decrypted2.len == 0) {
        try std.testing.expectEqualDeep("", decrypted2);
        try std.testing.expectEqualDeep(msg, decrypted1);
    } else {
        try std.testing.expect(false);
    }

    const user_data_ptr = try result.toBytesWithPrefix(allocator);
    const user_data_size = std.mem.readInt(u32, user_data_ptr[0..4], .little);
    const user_data = user_data_ptr[0 .. user_data_size + 4];
    defer allocator.free(user_data);
    const mixnet_data_list = &[_][]u8{
        decrypted_from_mixnet1,
        decrypted_from_mixnet2,
        decrypted_from_mixnet3,
    };

    const validated = try validate(
        allocator,
        user_data[4..],
        mixnet_data_list,
        mixnet_pk_list,
        trustee_pk_list,
        trustee_sk_list,
        max_size,
        1,
    );

    try std.testing.expectEqual(0, validated);
}

/// Decrypts a block of messages using a mixnet node's secret key.
/// Processes multiple encrypted messages in parallel and sorts the results
/// to remove ordering information (important for anonymity).
///
/// Args:
///   allocator: Memory allocator
///   key_secret: Secret key of this mixnet node
///   cypher_count: Number of encrypted messages in the block
///   cypher_block: Concatenated encrypted messages
///
/// Returns:
///   []u8: Decrypted and sorted message block
///   IdentityElementError: If key exchange results in identity element
///   AuthenticationError: If any message authentication fails
///   OutOfMemoryError: If memory allocation fails
pub fn decrypt_mixnet(
    allocator: std.mem.Allocator,
    key_secret: [32]u8,
    cypher_count: usize,
    cypher_block: []const u8,
) (IdentityElementError || AuthenticationError || OutOfMemoryError)![]u8 {
    assert(cypher_count > 0);
    assert(cypher_block.len > 0);
    assert(cypher_block.len % cypher_count == 0);
    const cypher_size = cypher_block.len / cypher_count;

    const decrypted_message_size = decrypted_bufsize(cypher_size);
    const decrypted_list = try allocator.alloc([]u8, cypher_count);
    defer allocator.free(decrypted_list);
    const decrypted_buf = try allocator.alloc(u8, decrypted_message_size * cypher_count);
    defer allocator.free(decrypted_buf);

    for (0..cypher_count) |i| {
        const cypher = cypher_block[i * cypher_size ..][0..cypher_size];
        const buf = decrypted_buf[i * decrypted_message_size ..][0..decrypted_message_size];

        // TODO: Handle error by ignoring message. It probably has to add a
        // 0-byte placeholder to keep the amount of cypher_count.
        _ = try decrypt_x25519(key_secret, cypher, buf);
        decrypted_list[i] = buf;
    }

    std.mem.sort([]u8, decrypted_list, {}, compareBytes);
    return std.mem.concat(allocator, u8, decrypted_list);
}

/// Comparison function for sorting byte arrays lexicographically.
/// Used by decrypt_mixnet to sort decrypted messages.
///
/// Args:
///   _: Unused context parameter
///   lhs: Left-hand side byte array
///   rhs: Right-hand side byte array
///
/// Returns:
///   bool: True if lhs < rhs lexicographically
fn compareBytes(_: void, lhs: []const u8, rhs: []const u8) bool {
    return std.mem.order(u8, lhs, rhs).compare(std.math.CompareOperator.lt);
}

/// Calculates the buffer size needed for trustee decryption.
///
/// Args:
///   cypher_block_size: Size of the encrypted block
///   cypher_count: Number of messages in the block
///
/// Returns:
///   usize: Required buffer size for decryption
pub fn decrypt_trustee_buf_size(cypher_block_size: usize, cypher_count: usize) usize {
    assert(cypher_count > 0);
    const cypher_size = cypher_block_size / cypher_count;
    return decrypted_bufsize(cypher_size) * cypher_count;
}

/// Performs the final decryption using combined trustee secret keys.
/// This is the last step in the mixnet decryption chain, revealing the
/// original voting messages after all mixnet processing is complete.
///
/// Args:
///   key_secret_list: Secret keys from all trustees
///   cypher_count: Number of encrypted messages
///   cypher_block: Block of encrypted messages from final mixnet node
///   buf: Pre-allocated buffer for decrypted messages
///
/// Returns:
///   []u8: Decrypted voting messages (concatenated, fixed-size blocks)
///   InvalidCypherError: If any cypher format is invalid
///   IdentityElementError: If key exchange results in identity element
///   WeakPublicKeyError: If any ephemeral key is weak
///   AuthenticationError: If any message authentication fails
pub fn decrypt_trustee(
    key_secret_list: []const EDCurve.scalar.CompressedScalar,
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

    const key_secret = combine_key_secret(key_secret_list);

    for (0..cypher_count) |i| {
        const cypher = cypher_block[i * cypher_size ..][0..cypher_size];
        // TODO: Ignore messages, that can not be decrypted.
        _ = try decrypt_x25519_no_clamp(key_secret, cypher, buf[i * decrypted_size ..]);
    }

    // TODO: Maybe return individual messages, since this is the cleartext. This
    // would either require the function to request an allocator or a list of
    // buffers (one for each message).
    return buf[0 .. cypher_count * decrypted_size];
}

test "decrypt many messages" {
    const allocator = std.testing.allocator;
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
        trustee_key1.key_secret,
        trustee_key2.key_secret,
        trustee_key3.key_secret,
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

    const cypher_size = comptime encrypt_full_buf_size(msg1.len, mixnet_pk_list.len);
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

    const decrypted_from_mixnet1 = try decrypt_mixnet(
        allocator,
        mixnet_key1.key_secret,
        msg_count,
        cypher_block[0 .. cypher1.len * 2],
    );
    defer allocator.free(decrypted_from_mixnet1);

    const decrypted_from_mixnet2 = try decrypt_mixnet(
        allocator,
        mixnet_key2.key_secret,
        msg_count,
        decrypted_from_mixnet1,
    );
    defer allocator.free(decrypted_from_mixnet2);

    const decrypted_from_mixnet3 = try decrypt_mixnet(
        allocator,
        mixnet_key3.key_secret,
        msg_count,
        decrypted_from_mixnet2,
    );
    defer allocator.free(decrypted_from_mixnet3);

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

/// Validates the integrity of the entire voting process.
/// Checks that users submitted valid encrypted votes and that mixnet nodes
/// processed them correctly without tampering. Uses zero-knowledge proofs
/// via fake message reconstruction.
///
/// Args:
///   allocator: Memory allocator
///   user_data_list: All user-submitted encrypted votes
///   mixnet_data_list: Output from each mixnet node
///   mixnet_key_public_list: Public keys of mixnet nodes
///   trustee_key_public_list: Public keys of trustees
///   trustee_key_secret_list: Secret keys of trustees (for verification)
///   max_size: Maximum message size
///   user_count: Number of users who voted
///
/// Returns:
///   i32: Validation result
///     0 = All validation passed
///     -N = User N-1 has submitted invalid data
///     +N = Mixnet node N-1 has tampered with data
pub fn validate(
    allocator: std.mem.Allocator,
    user_data_list: []const u8,
    mixnet_data_list: []const []const u8,
    mixnet_key_public_list: []const [32]u8,
    trustee_key_public_list: []const [32]u8,
    trustee_key_secret_list: []const [32]u8,
    max_size: u32,
    user_count: usize,
) !i32 {
    const seed_size = (mixnet_data_list.len + 1) * 32;
    const user_data_size = user_data_list.len / user_count;

    const seed_decrypt_buf = try allocator.alloc(u8, seed_size);
    defer allocator.free(seed_decrypt_buf);

    const key_secret_combined = combine_key_secret(trustee_key_secret_list);

    for (0..user_count) |i| {
        const cypher = user_data_list[i * user_data_size ..][0..user_data_size];
        const user_data = EncryptResult.fromBytes(cypher, mixnet_data_list.len, max_size);

        const seed = try decrypt_x25519_no_clamp(
            key_secret_combined,
            user_data.control_data,
            seed_decrypt_buf,
        );

        const fake_steps = try encrypt_fake_steps(
            allocator,
            mixnet_key_public_list,
            trustee_key_public_list,
            seed,
            max_size,
        );
        defer {
            for (fake_steps) |step| {
                allocator.free(step);
            }
            allocator.free(fake_steps);
        }

        if (!std.mem.eql(u8, fake_steps[0], user_data.cyphers[0]) and
            !std.mem.eql(u8, fake_steps[0], user_data.cyphers[1]))
        {
            return -@as(i32, @intCast(i + 1));
        }

        for (mixnet_data_list, 0..) |mixnet_data, j| {
            if (!in_mixnet_data(mixnet_data, fake_steps[j + 1], user_count * 2)) {
                return @intCast(j + 1);
            }
        }
    }
    return 0;
}

/// Checks if a specific piece of data exists within a sorted mixnet data block.
/// Uses binary search for efficient lookup in the sorted message array.
///
/// Args:
///   mixnet_data: Sorted block of messages from a mixnet node
///   data: Specific message to search for
///   message_count: Number of messages in the block
///
/// Returns:
///   bool: True if the data is found in the mixnet block
fn in_mixnet_data(mixnet_data: []const u8, data: []const u8, message_count: usize) bool {
    if (message_count == 0) return false;

    const message_length = mixnet_data.len / message_count;

    assert(data.len == message_length);

    var left: usize = 0;
    var right = message_count;

    while (left < right) {
        const mid = left + (right - left) / 2;
        const message_start = mid * message_length;
        const message_end = message_start + message_length;
        const current_message = mixnet_data[message_start..message_end];

        const order = std.mem.order(u8, data, current_message);

        switch (order) {
            .eq => return true,
            .lt => right = mid,
            .gt => left = mid + 1,
        }
    }

    return false;
}
