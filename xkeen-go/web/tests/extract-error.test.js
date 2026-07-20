// tests/extract-error.test.js — Tests for extractError utility
import { describe, it, expect } from 'vitest';
import { extractError } from '../src/utils/extract-error.js';

describe('extractError', () => {
    it('extracts from Error instance', () => {
        expect(extractError(new Error('boom'))).toBe('boom');
    });

    it('extracts from object with .message', () => {
        expect(extractError({ message: 'failed' })).toBe('failed');
    });

    it('extracts from object with .error', () => {
        expect(extractError({ error: 'denied' })).toBe('denied');
    });

    it('extracts from plain string', () => {
        expect(extractError('oops')).toBe('oops');
    });

    it('extracts from ApiError-like object (message + status)', () => {
        const apiErr = { name: 'ApiError', status: 500, message: 'server error', data: {} };
        expect(extractError(apiErr)).toBe('server error');
    });

    it('prefers .message over .error', () => {
        expect(extractError({ message: 'first', error: 'second' })).toBe('first');
    });

    it('returns Unknown error for falsy', () => {
        expect(extractError(null)).toBe('Unknown error');
        expect(extractError(undefined)).toBe('Unknown error');
        expect(extractError('')).toBe('Unknown error');
    });

    it('falls back to statusText', () => {
        expect(extractError({ status: 404, statusText: 'Not Found' })).toBe('Not Found');
    });

    it('falls back to String() for unknown types', () => {
        expect(extractError(42)).toBe('42');
    });
});
