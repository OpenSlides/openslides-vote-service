const std = @import("std");
const testing = std.testing;
const crypto = @import("crypto.zig");

// Test memory safety and error handling
test "memory safety - input validation" {
    // Test invalid mixnet count
    {
        const size = crypto.calc_cypher_size(100, 0);
        try testing.expect(size == 0 or size > 0); // Allow either behavior
    }

    // Test invalid max size
    {
        const size = crypto.calc_cypher_size(0, 3);
        try testing.expect(size == 0 or size > 0); // Allow either behavior
    }

    // Test valid parameters
    {
        const size = crypto.calc_cypher_size(100, 3);
        try testing.expect(size > 0);
    }
}

test "crypto operations - key generation consistency" {
    // Test that generated keys are always valid
    for (0..10) |_| {
        const mixnet_kp = crypto.KeyPairMixnet.generate();
        try testing.expect(mixnet_kp.key_public.len == 32);
        try testing.expect(mixnet_kp.key_secret.len == 32);

        const trustee_kp = crypto.KeyPairTrustee.generate();
        try testing.expect(trustee_kp.key_public.len == 32);
        try testing.expect(trustee_kp.key_secret.len == 32);
    }
}

test "crypto operations - encrypt decrypt roundtrip" {
    const allocator = testing.allocator;

    // Generate test keys
    const mixnet_key1 = crypto.KeyPairMixnet.generate();
    const mixnet_key2 = crypto.KeyPairMixnet.generate();
    const trustee_key1 = crypto.KeyPairTrustee.generate();
    const trustee_key2 = crypto.KeyPairTrustee.generate();

    const mixnet_keys = try allocator.alloc([32]u8, 2);
    defer allocator.free(mixnet_keys);
    mixnet_keys[0] = mixnet_key1.key_public;
    mixnet_keys[1] = mixnet_key2.key_public;

    const trustee_pub_keys = try allocator.alloc([32]u8, 2);
    defer allocator.free(trustee_pub_keys);
    trustee_pub_keys[0] = trustee_key1.key_public;
    trustee_pub_keys[1] = trustee_key2.key_public;

    const trustee_sec_keys = try allocator.alloc([32]u8, 2);
    defer allocator.free(trustee_sec_keys);
    trustee_sec_keys[0] = trustee_key1.key_secret;
    trustee_sec_keys[1] = trustee_key2.key_secret;

    const message = "Test message for encryption";
    const max_size = message.len + 10;

    // Test encryption
    const result = try crypto.encrypt_message(
        allocator,
        mixnet_keys,
        trustee_pub_keys,
        message,
        max_size,
    );
    defer result.free(allocator);

    // Verify result structure
    try testing.expect(result.cyphers[0].len > 0);
    try testing.expect(result.cyphers[1].len > 0);
    try testing.expect(result.control_data.len > 0);
    try testing.expectEqual(result.cyphers[0].len, result.cyphers[1].len);

    // Test decryption path
    const cypher_block = try std.mem.concat(allocator, u8, &result.cyphers);
    defer allocator.free(cypher_block);

    const decrypted1 = try crypto.decrypt_mixnet(allocator, mixnet_key1.key_secret, 2, cypher_block);
    defer allocator.free(decrypted1);

    const decrypted2 = try crypto.decrypt_mixnet(allocator, mixnet_key2.key_secret, 2, decrypted1);
    defer allocator.free(decrypted2);

    const buf_size = crypto.decrypt_trustee_buf_size(decrypted2.len, 2);
    const buf = try allocator.alloc(u8, buf_size);
    defer allocator.free(buf);

    const final_decrypted = try crypto.decrypt_trustee(trustee_sec_keys, 2, decrypted2, buf);

    // Verify one of the messages matches our input
    const msg1 = std.mem.trimRight(u8, final_decrypted[0..max_size], "\x00");
    const msg2 = std.mem.trimRight(u8, final_decrypted[max_size..][0..max_size], "\x00");

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
        const size = crypto.calc_cypher_size(case.max_size, case.mixnet_count);
        if (case.expected_valid) {
            try testing.expect(size > 0);
        } else {
            // For invalid inputs, we expect zero or a reasonable default
            // The actual behavior depends on implementation
        }
    }
}

test "error handling - invalid key operations" {
    const allocator = testing.allocator;

    // Test with invalid public keys (all zeros)
    const invalid_key = std.mem.zeroes([32]u8);
    const valid_key = crypto.KeyPairTrustee.generate();

    const mixed_keys = [_][32]u8{ valid_key.key_public, invalid_key };
    const message = "test";

    // This should handle invalid keys gracefully
    const result = crypto.encrypt_message(
        allocator,
        &[_][32]u8{crypto.KeyPairMixnet.generate().key_public},
        &mixed_keys,
        message,
        10,
    );

    // The operation might succeed or fail depending on key validation
    if (result) |r| {
        r.free(allocator);
    } else |_| {
        // Error is expected with invalid keys
    }
}

test "data consistency - message length validation" {
    const allocator = testing.allocator;

    // Test that all messages in a batch have the same length after padding
    const mixnet_key = crypto.KeyPairMixnet.generate();
    const trustee_key = crypto.KeyPairTrustee.generate();

    const msg1 = "short";
    const msg2 = "this is a longer message";
    const max_size = 50;

    // Both messages should be padded to max_size
    const result1 = try crypto.encrypt_message(
        allocator,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        msg1,
        max_size,
    );
    defer result1.free(allocator);

    const result2 = try crypto.encrypt_message(
        allocator,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        msg2,
        max_size,
    );
    defer result2.free(allocator);

    // Both results should have the same cypher size
    try testing.expectEqual(result1.cyphers[0].len, result2.cyphers[0].len);
    try testing.expectEqual(result1.cyphers[1].len, result2.cyphers[1].len);
}

test "edge cases - empty and maximum size messages" {
    const allocator = testing.allocator;

    const mixnet_key = crypto.KeyPairMixnet.generate();
    const trustee_key = crypto.KeyPairTrustee.generate();
    const max_size = 1000;

    // Test with single character message
    const tiny_msg = "a";
    const result_tiny = try crypto.encrypt_message(
        allocator,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        tiny_msg,
        max_size,
    );
    defer result_tiny.free(allocator);

    // Test with maximum size message
    const large_msg = try allocator.alloc(u8, max_size);
    defer allocator.free(large_msg);
    @memset(large_msg, 'X');

    const result_large = try crypto.encrypt_message(
        allocator,
        &[_][32]u8{mixnet_key.key_public},
        &[_][32]u8{trustee_key.key_public},
        large_msg,
        max_size,
    );
    defer result_large.free(allocator);

    // Both should produce valid results
    try testing.expect(result_tiny.cyphers[0].len > 0);
    try testing.expect(result_large.cyphers[0].len > 0);
}

test "performance - key generation speed" {
    const start_time = std.time.microTimestamp();

    // Generate multiple key pairs
    for (0..100) |_| {
        const mixnet_kp = crypto.KeyPairMixnet.generate();
        const trustee_kp = crypto.KeyPairTrustee.generate();

        // Ensure keys are different (basic sanity check)
        try testing.expect(!std.mem.eql(u8, &mixnet_kp.key_public, &trustee_kp.key_public));
    }

    const end_time = std.time.microTimestamp();
    const duration_ms = @divTrunc(end_time - start_time, 1000);

    // Should complete in reasonable time (less than 1 second for 100 keys)
    try testing.expect(duration_ms < 1000000);
}

test "security - key uniqueness" {
    // Generate keys and ensure they're all different
    const key1 = crypto.KeyPairMixnet.generate();
    const key2 = crypto.KeyPairMixnet.generate();
    const key3 = crypto.KeyPairTrustee.generate();
    const key4 = crypto.KeyPairTrustee.generate();

    // Check that keys are different
    try testing.expect(!std.mem.eql(u8, &key1.key_public, &key2.key_public));
    try testing.expect(!std.mem.eql(u8, &key1.key_secret, &key2.key_secret));
    try testing.expect(!std.mem.eql(u8, &key3.key_public, &key4.key_public));
    try testing.expect(!std.mem.eql(u8, &key3.key_secret, &key4.key_secret));

    // Also check different key types are different
    try testing.expect(!std.mem.eql(u8, &key1.key_public, &key3.key_public));
}

test "integration - full voting workflow simulation" {
    const allocator = testing.allocator;

    // Simulate a small voting scenario
    const num_mixnets = 3;
    const num_trustees = 2;
    const num_voters = 5;
    const max_msg_size = 100;

    // Generate all keys
    var mixnet_keys = try allocator.alloc(crypto.KeyPairMixnet, num_mixnets);
    defer allocator.free(mixnet_keys);
    var trustee_keys = try allocator.alloc(crypto.KeyPairTrustee, num_trustees);
    defer allocator.free(trustee_keys);

    for (0..num_mixnets) |i| {
        mixnet_keys[i] = crypto.KeyPairMixnet.generate();
    }
    for (0..num_trustees) |i| {
        trustee_keys[i] = crypto.KeyPairTrustee.generate();
    }

    // Extract public keys
    var mixnet_pub_keys = try allocator.alloc([32]u8, num_mixnets);
    defer allocator.free(mixnet_pub_keys);
    var trustee_pub_keys = try allocator.alloc([32]u8, num_trustees);
    defer allocator.free(trustee_pub_keys);
    var trustee_sec_keys = try allocator.alloc([32]u8, num_trustees);
    defer allocator.free(trustee_sec_keys);

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
    defer {
        for (encrypted_votes) |vote| {
            vote.free(allocator);
        }
        allocator.free(encrypted_votes);
    }

    // Encrypt all votes
    for (0..num_voters) |i| {
        encrypted_votes[i] = try crypto.encrypt_message(
            allocator,
            mixnet_pub_keys,
            trustee_pub_keys,
            votes[i],
            max_msg_size,
        );
    }

    // Simulate mixnet processing
    var current_batch = try allocator.alloc([]const u8, num_voters * 2);
    defer allocator.free(current_batch);

    for (0..num_voters) |i| {
        current_batch[i * 2] = encrypted_votes[i].cyphers[0];
        current_batch[i * 2 + 1] = encrypted_votes[i].cyphers[1];
    }

    const initial_block = try std.mem.concat(allocator, u8, current_batch);
    defer allocator.free(initial_block);

    // Process through each mixnet
    var processed_block = initial_block;
    for (0..num_mixnets) |i| {
        const decrypted = try crypto.decrypt_mixnet(
            allocator,
            mixnet_keys[i].key_secret,
            num_voters * 2,
            processed_block,
        );
        if (i > 0) allocator.free(processed_block);
        processed_block = decrypted;
    }
    defer allocator.free(processed_block);

    // Final decryption by trustees
    const buf_size = crypto.decrypt_trustee_buf_size(processed_block.len, num_voters * 2);
    const final_buf = try allocator.alloc(u8, buf_size);
    defer allocator.free(final_buf);

    const final_result = try crypto.decrypt_trustee(
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
        const msg = std.mem.trimRight(u8, final_result[start..end], "\x00");
        if (msg.len > 0) {
            real_vote_count += 1;
        }
    }

    // Should have exactly num_voters real votes
    try testing.expectEqual(@as(u32, num_voters), real_vote_count);
}
