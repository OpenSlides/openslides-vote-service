const std = @import("std");
const json = std.json;
const Allocator = std.mem.Allocator;
const base64 = std.base64;

// Error types for parsing
const ParseError = error{
    UnknownEventType,
    InvalidJson,
    MissingField,
    HashMismatch,
    HashRequiredForNonStartEvents,
    InvalidBase64Hash,
};

// Tagged Union for all Event types with flattened structures
const ParsedEvent = union(enum) {
    start: struct {
        time: []const u8,
        vote_user_ids: []u32,
        poll_worker_ids: []u32,
        vote_size: u32,
    },
    stop: struct {
        time: []const u8,
        hash: [32]u8,
    },
    publish_key_public: struct {
        time: []const u8,
        hash: [32]u8,
        user_id: u32,
        key_public: []const u8,
    },
    publish_key_secret_list: struct {
        time: []const u8,
        hash: [32]u8,
        key_secret_list: [][]const u8,
    },
    vote: struct {
        time: []const u8,
        hash: [32]u8,
        user_id: u32,
        vote: []const u8,
    },

    pub fn free(self: ParsedEvent, allocator: Allocator) void {
        freeParsedEvent(allocator, self);
    }
};

// Enum for Event types
const EventType = enum {
    start,
    stop,
    publish_key_public,
    publish_key_secret_list,
    vote,

    fn fromString(str: []const u8) ParseError!EventType {
        const hash = std.hash_map.hashString(str);
        switch (hash) {
            std.hash_map.hashString("start") => return .start,
            std.hash_map.hashString("stop") => return .stop,
            std.hash_map.hashString("publish_key_public") => return .publish_key_public,
            std.hash_map.hashString("publish_key_secret_list") => return .publish_key_secret_list,
            std.hash_map.hashString("vote") => return .vote,
            else => return ParseError.UnknownEventType,
        }
    }
};

// Hash function for events
pub fn hashEvent(event: []const u8) [32]u8 {
    var hasher = std.crypto.hash.sha2.Sha256.init(.{});
    hasher.update(event);
    return hasher.finalResult();
}

// Helper function to decode base64 hash
fn decodeBase64Hash(hash_str: []const u8) ![32]u8 {
    var hash_bytes: [32]u8 = undefined;
    const decoder = base64.standard.Decoder;

    if (hash_str.len != base64.standard.Encoder.calcSize(32)) {
        return ParseError.InvalidBase64Hash;
    }

    try decoder.decode(&hash_bytes, hash_str);
    return hash_bytes;
}

// Main function to parse events with hash validation
pub fn parseEvent(allocator: Allocator, json_string: []const u8, last_hash: ?[32]u8) !ParsedEvent {
    // Parse JSON
    const parsed = json.parseFromSlice(json.Value, allocator, json_string, .{}) catch {
        return ParseError.InvalidJson;
    };
    defer parsed.deinit();

    const root = parsed.value.object;

    // Extract event fields
    const event_time = root.get("time") orelse return ParseError.MissingField;
    const event_type_str = root.get("type") orelse return ParseError.MissingField;
    const message_value = root.get("message");
    const hash_value = root.get("hash");

    const time_string = try allocator.dupe(u8, event_time.string);
    errdefer allocator.free(time_string); // Free time_string on error

    const event_type = try EventType.fromString(event_type_str.string);

    // Hash validation for non-start events
    if (event_type != .start) {
        // Non-start events require last_hash to be provided
        const expected_hash = last_hash orelse return ParseError.HashRequiredForNonStartEvents;

        // Non-start events must have a hash field in JSON
        const json_hash = hash_value orelse return ParseError.MissingField;

        // Decode the base64 hash from JSON
        const decoded_hash = try decodeBase64Hash(json_hash.string);

        // Validate hash matches
        if (!std.mem.eql(u8, &expected_hash, &decoded_hash)) {
            return ParseError.HashMismatch;
        }
    }

    // Switch based on event type
    switch (event_type) {
        .start => {
            const msg = message_value orelse return ParseError.MissingField;
            const msg_obj = msg.object;

            const vote_user_ids_json = msg_obj.get("vote_user_ids") orelse return ParseError.MissingField;
            const poll_worker_ids_json = msg_obj.get("poll_worker_ids") orelse return ParseError.MissingField;
            const vote_size_json = msg_obj.get("vote_size") orelse return ParseError.MissingField;

            // Allocate and copy arrays
            var vote_user_ids = try allocator.alloc(u32, vote_user_ids_json.array.items.len);
            errdefer allocator.free(vote_user_ids); // Free on error
            for (vote_user_ids_json.array.items, 0..) |item, i| {
                vote_user_ids[i] = @intCast(item.integer);
            }

            var poll_worker_ids = try allocator.alloc(u32, poll_worker_ids_json.array.items.len);
            errdefer allocator.free(poll_worker_ids); // Free on error
            for (poll_worker_ids_json.array.items, 0..) |item, i| {
                poll_worker_ids[i] = @intCast(item.integer);
            }

            return ParsedEvent{
                .start = .{
                    .time = time_string,
                    .vote_user_ids = vote_user_ids,
                    .poll_worker_ids = poll_worker_ids,
                    .vote_size = @intCast(vote_size_json.integer),
                },
            };
        },

        .stop => {
            const hash_string = if (hash_value) |h| try decodeBase64Hash(h.string) else return ParseError.MissingField;

            return ParsedEvent{
                .stop = .{
                    .time = time_string,
                    .hash = hash_string,
                },
            };
        },

        .publish_key_public => {
            const msg = message_value orelse return ParseError.MissingField;
            const msg_obj = msg.object;
            const hash_bytes = if (hash_value) |h| try decodeBase64Hash(h.string) else return ParseError.MissingField;

            const user_id_json = msg_obj.get("user_id") orelse return ParseError.MissingField; // Changed from user_ids to user_id
            const key_public_json = msg_obj.get("key_public") orelse return ParseError.MissingField;

            const key_public = try allocator.dupe(u8, key_public_json.string);

            return ParsedEvent{
                .publish_key_public = .{
                    .time = time_string,
                    .hash = hash_bytes,
                    .user_id = @intCast(user_id_json.integer), // Changed from user_ids to user_id
                    .key_public = key_public,
                },
            };
        },

        .publish_key_secret_list => {
            const msg = message_value orelse return ParseError.MissingField;
            const msg_obj = msg.object;
            const hash_bytes = if (hash_value) |h| try decodeBase64Hash(h.string) else return ParseError.MissingField;

            const key_secret_list_json = msg_obj.get("key_secret_list") orelse return ParseError.MissingField;

            var key_secret_list = try allocator.alloc([]const u8, key_secret_list_json.array.items.len);
            errdefer allocator.free(key_secret_list); // Free on error

            var allocated_count: usize = 0;
            errdefer {
                // Free all allocated strings on error
                for (key_secret_list[0..allocated_count]) |key| {
                    allocator.free(key);
                }
            }

            for (key_secret_list_json.array.items, 0..) |item, i| {
                key_secret_list[i] = try allocator.dupe(u8, item.string);
                allocated_count += 1;
            }

            return ParsedEvent{
                .publish_key_secret_list = .{
                    .time = time_string,
                    .hash = hash_bytes,
                    .key_secret_list = key_secret_list,
                },
            };
        },

        .vote => {
            const msg = message_value orelse return ParseError.MissingField;
            const msg_obj = msg.object;
            const hash_bytes = if (hash_value) |h| try decodeBase64Hash(h.string) else return ParseError.MissingField;

            const user_id_json = msg_obj.get("user_id") orelse return ParseError.MissingField; // Changed from user_ids to user_id
            const vote_json = msg_obj.get("vote") orelse return ParseError.MissingField;

            const vote_string = try allocator.dupe(u8, vote_json.string);

            return ParsedEvent{
                .vote = .{
                    .time = time_string,
                    .hash = hash_bytes,
                    .user_id = @intCast(user_id_json.integer), // Changed from user_ids to user_id
                    .vote = vote_string,
                },
            };
        },
    }
}

// Helper function to free memory
fn freeParsedEvent(allocator: Allocator, event: ParsedEvent) void {
    switch (event) {
        .start => |start| {
            allocator.free(start.time);
            allocator.free(start.vote_user_ids);
            allocator.free(start.poll_worker_ids);
        },
        .stop => |stop| {
            allocator.free(stop.time);
            // hash is now [32]u8, no need to free
        },
        .publish_key_public => |pub_key| {
            allocator.free(pub_key.time);
            // hash is now [32]u8, no need to free
            allocator.free(pub_key.key_public);
        },
        .publish_key_secret_list => |secret_list| {
            allocator.free(secret_list.time);
            // hash is now [32]u8, no need to free
            for (secret_list.key_secret_list) |key| {
                allocator.free(key);
            }
            allocator.free(secret_list.key_secret_list);
        },
        .vote => |vote| {
            allocator.free(vote.time);
            // hash is now [32]u8, no need to free
            allocator.free(vote.vote);
        },
    }
}

// Tests that demonstrate usage
const testing = std.testing;

test "parse start event without hash validation" {
    const allocator = testing.allocator;

    const start_json =
        \\{
        \\  "time": "2025-06-15T12:00:00Z",
        \\  "type": "start",
        \\  "message": {
        \\    "vote_user_ids": [1, 2, 3],
        \\    "poll_worker_ids": [10, 20],
        \\    "vote_size": 100
        \\  }
        \\}
    ;

    const event = try parseEvent(allocator, start_json, null);
    defer freeParsedEvent(allocator, event);

    switch (event) {
        .start => |start| {
            try testing.expectEqualStrings("2025-06-15T12:00:00Z", start.time);
            try testing.expectEqual(@as(u32, 100), start.vote_size);
            try testing.expectEqual(@as(usize, 3), start.vote_user_ids.len);
            try testing.expectEqual(@as(u32, 1), start.vote_user_ids[0]);
            try testing.expectEqual(@as(u32, 2), start.vote_user_ids[1]);
            try testing.expectEqual(@as(u32, 3), start.vote_user_ids[2]);
            try testing.expectEqual(@as(usize, 2), start.poll_worker_ids.len);
            try testing.expectEqual(@as(u32, 10), start.poll_worker_ids[0]);
            try testing.expectEqual(@as(u32, 20), start.poll_worker_ids[1]);
        },
        else => try testing.expect(false), // Should be start event
    }
}

test "parse vote event with valid hash" {
    const allocator = testing.allocator;

    const vote_json =
        \\{
        \\  "time": "2025-06-15T12:30:00Z",
        \\  "type": "vote",
        \\  "message": {
        \\    "user_id": 42,
        \\    "vote": "candidate_a"
        \\  },
        \\  "hash": "YWJjMTIzZGVmNDU2MTIzNDU2Nzg5MGFiY2RlZjEyMzQ="
        \\}
    ;

    // Create a hash that matches the base64 encoded value
    const expected_hash = try decodeBase64Hash("YWJjMTIzZGVmNDU2MTIzNDU2Nzg5MGFiY2RlZjEyMzQ=");
    const event = try parseEvent(allocator, vote_json, expected_hash);
    defer freeParsedEvent(allocator, event);

    switch (event) {
        .vote => |vote| {
            try testing.expectEqualStrings("2025-06-15T12:30:00Z", vote.time);
            try testing.expectEqual(expected_hash, vote.hash);
            try testing.expectEqual(@as(u32, 42), vote.user_id); // Changed from user_ids to user_id
            try testing.expectEqualStrings("candidate_a", vote.vote);
        },
        else => try testing.expect(false), // Should be vote event
    }
}

test "parse publish_key_public event with hash validation" {
    const allocator = testing.allocator;

    const pub_key_json =
        \\{
        \\  "time": "2025-06-15T13:00:00Z",
        \\  "type": "publish_key_public",
        \\  "message": {
        \\    "user_id": 123,
        \\    "key_public": "-----BEGIN PUBLIC KEY-----\nMFkw...IDAQAB\n-----END PUBLIC KEY-----"
        \\  },
        \\  "hash": "ZGVmNzg5Z2hpMDEyMzQ1Njc4OTBhYmNkZWYxMjM0NTY="
        \\}
    ;

    const expected_hash = try decodeBase64Hash("ZGVmNzg5Z2hpMDEyMzQ1Njc4OTBhYmNkZWYxMjM0NTY=");
    const event = try parseEvent(allocator, pub_key_json, expected_hash);
    defer freeParsedEvent(allocator, event);

    switch (event) {
        .publish_key_public => |pub_key| {
            try testing.expectEqualStrings("2025-06-15T13:00:00Z", pub_key.time);
            try testing.expectEqual(expected_hash, pub_key.hash);
            try testing.expectEqual(@as(u32, 123), pub_key.user_id); // Changed from user_ids to user_id
            try testing.expectEqualStrings("-----BEGIN PUBLIC KEY-----\nMFkw...IDAQAB\n-----END PUBLIC KEY-----", pub_key.key_public);
        },
        else => try testing.expect(false), // Should be publish_key_public event
    }
}

test "parse stop event" {
    const allocator = testing.allocator;

    const stop_json =
        \\{
        \\  "time": "2025-06-15T14:00:00Z",
        \\  "type": "stop",
        \\  "hash": "ZmluYWwxMjNoYXNoMTIzNDU2Nzg5MGFiY2RlZjEyMzQ="
        \\}
    ;

    const expected_hash = try decodeBase64Hash("ZmluYWwxMjNoYXNoMTIzNDU2Nzg5MGFiY2RlZjEyMzQ=");
    const event = try parseEvent(allocator, stop_json, expected_hash);
    defer freeParsedEvent(allocator, event);

    switch (event) {
        .stop => |stop| {
            try testing.expectEqualStrings("2025-06-15T14:00:00Z", stop.time);
            try testing.expectEqual(expected_hash, stop.hash);
        },
        else => try testing.expect(false), // Should be stop event
    }
}

test "hash mismatch error" {
    const allocator = testing.allocator;

    const vote_json =
        \\{
        \\  "time": "2025-06-15T12:30:00Z",
        \\  "type": "vote",
        \\  "message": {
        \\    "user_id": 42,
        \\    "vote": "candidate_a"
        \\  },
        \\  "hash": "YWJjMTIzZGVmNDU2MTIzNDU2Nzg5MGFiY2RlZjEyMzQ="
        \\}
    ;

    // Create a wrong hash (different from the one in JSON)
    const wrong_hash: [32]u8 = [_]u8{0} ** 32; // All zeros
    const result = parseEvent(allocator, vote_json, wrong_hash);
    try testing.expectError(ParseError.HashMismatch, result);
}

test "unknown event type error" {
    const allocator = testing.allocator;

    const unknown_json =
        \\{
        \\  "time": "2025-06-15T12:30:00Z",
        \\  "type": "unknown_type",
        \\  "message": {},
        \\  "hash": "abc123def456"
        \\}
    ;

    const expected_hash = try decodeBase64Hash("ZmluYWwxMjNoYXNoMTIzNDU2Nzg5MGFiY2RlZjEyMzQ=");
    const result = parseEvent(allocator, unknown_json, expected_hash);
    try testing.expectError(ParseError.UnknownEventType, result);
}

test "invalid json error" {
    const allocator = testing.allocator;

    const invalid_json = "{ invalid json }";

    const result = parseEvent(allocator, invalid_json, null);
    try testing.expectError(ParseError.InvalidJson, result);
}
