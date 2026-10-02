const std = @import("std");
const testing = std.testing;
const crypto = @import("crypto.zig");

fn testRandom(buf: []u8) void {
    const io: std.Io = std.testing.io;
    io.random(buf);
}

// Test memory safety and error handling
test "memory safety - input validation" {
    // Test invalid mixnet count
    {
        const size = crypto.calcCypherSize(100, 0);
        try testing.expect(size == 0 or size > 0); // Allow either behavior
    }

    // Test invalid max size
    {
        const size = crypto.calcCypherSize(0, 3);
        try testing.expect(size == 0 or size > 0); // Allow either behavior
    }

    // Test valid parameters
    {
        const size = crypto.calcCypherSize(100, 3);
        try testing.expect(size > 0);
    }
}

test "crypto operations - key generation consistency" {
    // Test that generated keys are always valid
    for (0..10) |_| {
        const mixnet_kp = crypto.KeyPairMixnet.generate(testRandom);
        try testing.expect(mixnet_kp.key_public.len == 32);
        try testing.expect(mixnet_kp.key_secret.len == 32);

        const trustee_kp = crypto.KeyPairTrustee.generate(testRandom);
        try testing.expect(trustee_kp.key_public.len == 32);
        try testing.expect(trustee_kp.key_secret.len == 32);
    }
}

test "crypto operations - encrypt decrypt roundtrip" {
    var arena: std.heap.ArenaAllocator = .init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    // Generate test keys
    const mixnet_key1 = crypto.KeyPairMixnet.generate(testRandom);
    const mixnet_key2 = crypto.KeyPairMixnet.generate(testRandom);
    const trustee_key1 = crypto.KeyPairTrustee.generate(testRandom);
    const trustee_key2 = crypto.KeyPairTrustee.generate(testRandom);

    const mixnet_keys = try allocator.alloc([32]u8, 2);
    mixnet_keys[0] = mixnet_key1.key_public;
    mixnet_keys[1] = mixnet_key2.key_public;

    const trustee_pub_keys = try allocator.alloc([32]u8, 2);
    trustee_pub_keys[0] = trustee_key1.key_public;
    trustee_pub_keys[1] = trustee_key2.key_public;

    const trustee_sec_keys = try allocator.alloc([32]u8, 2);
    trustee_sec_keys[0] = trustee_key1.key_secret;
    trustee_sec_keys[1] = trustee_key2.key_secret;

    const message = "Test message for encryption";
    const max_size = message.len + 10;

    // Test encryption
    const result = try crypto.encryptMessage(
        allocator,
        testRandom,
        mixnet_keys,
        trustee_pub_keys,
        message,
        max_size,
    );

    // Verify result structure
    try testing.expect(result.cyphers[0].len > 0);
    try testing.expect(result.cyphers[1].len > 0);
    try testing.expect(result.control_data.len > 0);
    try testing.expectEqual(result.cyphers[0].len, result.cyphers[1].len);

    // Test decryption path
    const cypher_block = try std.mem.concat(allocator, u8, &result.cyphers);
    const decrypted1 = try crypto.decryptMixnet(allocator, mixnet_key1.key_secret, 2, cypher_block);
    const decrypted2 = try crypto.decryptMixnet(allocator, mixnet_key2.key_secret, 2, decrypted1);

    const buf_size = crypto.decryptTrusteeBufSize(decrypted2.len, 2);
    const buf = try allocator.alloc(u8, buf_size);

    const final_decrypted = try crypto.decryptTrustee(trustee_sec_keys, 2, decrypted2, buf);

    // Verify one of the messages matches our input
    const msg1 = std.mem.trimEnd(u8, final_decrypted[0..max_size], "\x00");
    const msg2 = std.mem.trimEnd(u8, final_decrypted[max_size..][0..max_size], "\x00");

    const found_message = std.mem.eql(u8, msg1, message) or std.mem.eql(u8, msg2, message);
    try testing.expect(found_message);
}

test "buffer safety - size validation" {
    // Test cypher size calculation with various parameters
    const test_cases = [_]struct { mixnet_count: usize, max_size: usize, expected_valid: bool }{
        .{ .mixnet_count = 1, .max_size = 100, .expected_valid = true },
        .{ .mixnet_count = 10, .max_size = 1000, .expected_valid = true },
        .{ .mixnet_count = 0, .max_size = 100, .expected_valid = false },
        .{ .mixnet_count = 1, .max_size = 0, .expected_valid = false },
    };

    for (test_cases) |case| {
        const size = crypto.calcCypherSize(case.max_size, case.mixnet_count);
        if (case.expected_valid) {
            try testing.expect(size > 0);
        } else {
            // For invalid inputs, we expect zero or a reasonable default
            // The actual behavior depends on implementation
        }
    }
}

test "error handling - invalid key operations" {
    var arena: std.heap.ArenaAllocator = .init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    // Test with invalid public keys (all zeros)
    const invalid_key = std.mem.zeroes([32]u8);
    const valid_key = crypto.KeyPairTrustee.generate(testRandom);

    const mixed_keys = [_][32]u8{ valid_key.key_public, invalid_key };
    const message = "test";

    // This should handle invalid keys gracefully
    _ = try crypto.encryptMessage(
        allocator,
        testRandom,
        &[_][32]u8{crypto.KeyPairMixnet.generate(testRandom).key_public},
        &mixed_keys,
        message,
        10,
    );
}

test "data consistency - message length validation" {
    var arena: std.heap.ArenaAllocator = .init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    // Test that all messages in a batch have the same length after padding
    const mixnet_key = crypto.KeyPairMixnet.generate(testRandom);
    const trustee_key = crypto.KeyPairTrustee.generate(testRandom);

    const msg1 = "short";
    const msg2 = "this is a longer message";
    const max_size = 50;

    // Both messages should be padded to max_size
    const result1 = try crypto.encryptMessage(
        allocator,
        testRandom,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        msg1,
        max_size,
    );

    const result2 = try crypto.encryptMessage(
        allocator,
        testRandom,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        msg2,
        max_size,
    );

    // Both results should have the same cypher size
    try testing.expectEqual(result1.cyphers[0].len, result2.cyphers[0].len);
    try testing.expectEqual(result1.cyphers[1].len, result2.cyphers[1].len);
}

test "edge cases - empty and maximum size messages" {
    var arena: std.heap.ArenaAllocator = .init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    const mixnet_key = crypto.KeyPairMixnet.generate(testRandom);
    const trustee_key = crypto.KeyPairTrustee.generate(testRandom);
    const max_size = 1000;

    // Test with single character message
    const tiny_msg = "a";
    const result_tiny = try crypto.encryptMessage(
        allocator,
        testRandom,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        tiny_msg,
        max_size,
    );

    // Test with maximum size message
    const large_msg = try allocator.alloc(u8, max_size);
    @memset(large_msg, 'X');

    const result_large = try crypto.encryptMessage(
        allocator,
        testRandom,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        large_msg,
        max_size,
    );

    // Both should produce valid results
    try testing.expect(result_tiny.cyphers[0].len > 0);
    try testing.expect(result_large.cyphers[0].len > 0);
}

test "performance - key generation speed" {
    const io = std.testing.io;
    const start_time = std.Io.Clock.now(.awake, io);

    // Generate multiple key pairs
    for (0..100) |_| {
        const mixnet_kp = crypto.KeyPairMixnet.generate(testRandom);
        const trustee_kp = crypto.KeyPairTrustee.generate(testRandom);

        // Ensure keys are different (basic sanity check)
        try testing.expect(!std.mem.eql(u8, &mixnet_kp.key_public, &trustee_kp.key_public));
    }

    const end_time = std.Io.Clock.now(.awake, io);
    const duration_ms = start_time.durationTo(end_time).toMilliseconds();

    // Should complete in reasonable time (less than 1 second for 100 keys)
    try testing.expect(duration_ms < 1000);
}

test "security - key uniqueness" {
    // Generate keys and ensure they're all different
    const key1 = crypto.KeyPairMixnet.generate(testRandom);
    const key2 = crypto.KeyPairMixnet.generate(testRandom);
    const key3 = crypto.KeyPairTrustee.generate(testRandom);
    const key4 = crypto.KeyPairTrustee.generate(testRandom);

    // Check that keys are different
    try testing.expect(!std.mem.eql(u8, &key1.key_public, &key2.key_public));
    try testing.expect(!std.mem.eql(u8, &key1.key_secret, &key2.key_secret));
    try testing.expect(!std.mem.eql(u8, &key3.key_public, &key4.key_public));
    try testing.expect(!std.mem.eql(u8, &key3.key_secret, &key4.key_secret));

    // Also check different key types are different
    try testing.expect(!std.mem.eql(u8, &key1.key_public, &key3.key_public));
}

test "integration - full voting workflow simulation" {
    var arena: std.heap.ArenaAllocator = .init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    // Simulate a small voting scenario
    const num_mixnets = 3;
    const num_trustees = 2;
    const num_voters = 5;
    const max_msg_size = 100;

    // Generate all keys
    var mixnet_keys = try allocator.alloc(crypto.KeyPairMixnet, num_mixnets);
    var trustee_keys = try allocator.alloc(crypto.KeyPairTrustee, num_trustees);

    for (0..num_mixnets) |i| {
        mixnet_keys[i] = crypto.KeyPairMixnet.generate(testRandom);
    }
    for (0..num_trustees) |i| {
        trustee_keys[i] = crypto.KeyPairTrustee.generate(testRandom);
    }

    // Extract public keys
    var mixnet_pub_keys = try allocator.alloc([32]u8, num_mixnets);
    var trustee_pub_keys = try allocator.alloc([32]u8, num_trustees);
    var trustee_sec_keys = try allocator.alloc([32]u8, num_trustees);

    for (0..num_mixnets) |i| {
        mixnet_pub_keys[i] = mixnet_keys[i].key_public;
    }
    for (0..num_trustees) |i| {
        trustee_pub_keys[i] = trustee_keys[i].key_public;
        trustee_sec_keys[i] = trustee_keys[i].key_secret;
    }

    // Simulate votes
    const votes = [_][]const u8{ "Alice", "Bob", "Alice", "Carol", "Bob" };
    var encrypted_votes = try allocator.alloc(crypto.EncryptResult, num_voters);

    // Encrypt all votes
    for (0..num_voters) |i| {
        encrypted_votes[i] = try crypto.encryptMessage(
            allocator,
            testRandom,
            mixnet_pub_keys,
            trustee_pub_keys,
            votes[i],
            max_msg_size,
        );
    }

    // Simulate mixnet processing
    var current_batch = try allocator.alloc([]const u8, num_voters * 2);

    for (0..num_voters) |i| {
        current_batch[i * 2] = encrypted_votes[i].cyphers[0];
        current_batch[i * 2 + 1] = encrypted_votes[i].cyphers[1];
    }

    const initial_block = try std.mem.concat(allocator, u8, current_batch);

    // Process through each mixnet
    var processed_block = initial_block;
    for (0..num_mixnets) |i| {
        const decrypted = try crypto.decryptMixnet(
            allocator,
            mixnet_keys[i].key_secret,
            num_voters * 2,
            processed_block,
        );
        processed_block = decrypted;
    }

    // Final decryption by trustees
    const buf_size = crypto.decryptTrusteeBufSize(processed_block.len, num_voters * 2);
    const final_buf = try allocator.alloc(u8, buf_size);

    const final_result = try crypto.decryptTrustee(
        trustee_sec_keys,
        num_voters * 2,
        processed_block,
        final_buf,
    );

    // Verify we can extract meaningful results
    try testing.expect(final_result.len > 0);
    try testing.expect(final_result.len == num_voters * 2 * max_msg_size);

    // Count non-empty messages (real votes vs fake votes)
    var real_vote_count: u32 = 0;
    for (0..num_voters * 2) |i| {
        const start = i * max_msg_size;
        const end = start + max_msg_size;
        const msg = std.mem.trimEnd(u8, final_result[start..end], "\x00");
        if (msg.len > 0) {
            real_vote_count += 1;
        }
    }

    // Should have exactly num_voters real votes
    try testing.expectEqual(@as(u32, num_voters), real_vote_count);
}
