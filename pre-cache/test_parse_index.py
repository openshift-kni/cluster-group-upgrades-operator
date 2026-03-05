#!/usr/bin/env python3
"""
Unit tests for parse_index.py - OLM catalog parsing and channel head detection
"""

import unittest
import tempfile
import json
import sys
import os
from io import StringIO

# Import the module under test
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import parse_index


class TestExtractImages(unittest.TestCase):
    """Test the extract_images function, particularly channel head detection"""

    def setUp(self):
        """Set up test fixtures"""
        self.maxDiff = None

    def create_args(self, operators_spec, rendered_index_data, output_file):
        """Helper to create mock args object"""
        class MockArgs:
            def __init__(self, operators_spec_file, rendered_index_file, img_list_file):
                self.operators_spec_file = MockFile(operators_spec_file)
                self.rendered_index = MockFile(rendered_index_file)
                self.img_list_file = MockFile(img_list_file)

        class MockFile:
            def __init__(self, path):
                self.name = path

        return MockArgs(operators_spec, rendered_index_data, output_file)

    def test_channel_head_selection_simple_chain(self):
        """Test that channel head is correctly identified in a simple upgrade chain"""
        # Create a channel with linear upgrade path: v1.0.0 -> v1.0.1 -> v1.0.2 (head)
        objects = [
            {
                "schema": "olm.channel",
                "package": "myoperator",
                "name": "stable",
                "entries": [
                    {"name": "myoperator.v1.0.0"},
                    {"name": "myoperator.v1.0.1", "replaces": "myoperator.v1.0.0"},
                    {"name": "myoperator.v1.0.2", "replaces": "myoperator.v1.0.1"}
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "myoperator.v1.0.2",
                "package": "myoperator",
                "relatedImages": [
                    {"image": "quay.io/myoperator:v1.0.2"},
                    {"image": "quay.io/myoperator-helper:v1.0.2"}
                ]
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("myoperator:stable\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            # Should select v1.0.2 (head), not last in array
            self.assertIn("quay.io/myoperator:v1.0.2", images)
            self.assertIn("quay.io/myoperator-helper:v1.0.2", images)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)

    def test_channel_head_last_entry_not_head(self):
        """Test the bug fix: last entry in array is NOT the channel head"""
        # Create a channel where last entry is actually an old version
        # Upgrade path: v1.0.0 -> v1.0.2 -> v1.0.3 (head)
        # But array order is: [v1.0.0, v1.0.3, v1.0.2] (v1.0.2 is last, but old)
        objects = [
            {
                "schema": "olm.channel",
                "package": "myoperator",
                "name": "stable",
                "entries": [
                    {"name": "myoperator.v1.0.0"},
                    {"name": "myoperator.v1.0.3", "replaces": "myoperator.v1.0.2"},  # This is the head
                    {"name": "myoperator.v1.0.2", "replaces": "myoperator.v1.0.0"}   # Last in array, but not head
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "myoperator.v1.0.3",
                "package": "myoperator",
                "relatedImages": [
                    {"image": "quay.io/myoperator:v1.0.3"}
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "myoperator.v1.0.2",
                "package": "myoperator",
                "relatedImages": [
                    {"image": "quay.io/myoperator:v1.0.2"}
                ]
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("myoperator:stable\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            # Should select v1.0.3 (the actual head), NOT v1.0.2 (last in array)
            self.assertIn("quay.io/myoperator:v1.0.3", images)
            self.assertNotIn("quay.io/myoperator:v1.0.2", images)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)

    def test_channel_head_with_skips(self):
        """Test channel head detection with skip entries"""
        # Create a channel with skips
        objects = [
            {
                "schema": "olm.channel",
                "package": "myoperator",
                "name": "stable",
                "entries": [
                    {"name": "myoperator.v1.0.0"},
                    {"name": "myoperator.v1.0.1", "replaces": "myoperator.v1.0.0"},
                    {"name": "myoperator.v1.0.2", "replaces": "myoperator.v1.0.0", "skips": ["myoperator.v1.0.1"]}
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "myoperator.v1.0.2",
                "package": "myoperator",
                "relatedImages": [
                    {"image": "quay.io/myoperator:v1.0.2"}
                ]
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("myoperator:stable\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            # Should select v1.0.2 (head), even though v1.0.1 is also not replaced
            # Note: v1.0.1 is skipped but not replaced, so it would also be detected as head
            # In real catalogs, skipped versions shouldn't be heads
            self.assertIn("quay.io/myoperator:v1.0.2", images)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)

    def test_multiple_packages_multiple_channels(self):
        """Test handling multiple packages and channels"""
        objects = [
            {
                "schema": "olm.channel",
                "package": "operator-a",
                "name": "stable",
                "entries": [
                    {"name": "operator-a.v1.0.0"},
                    {"name": "operator-a.v1.0.1", "replaces": "operator-a.v1.0.0"}
                ]
            },
            {
                "schema": "olm.channel",
                "package": "operator-b",
                "name": "fast",
                "entries": [
                    {"name": "operator-b.v2.0.0"},
                    {"name": "operator-b.v2.0.1", "replaces": "operator-b.v2.0.0"}
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "operator-a.v1.0.1",
                "package": "operator-a",
                "relatedImages": [
                    {"image": "quay.io/operator-a:v1.0.1"}
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "operator-b.v2.0.1",
                "package": "operator-b",
                "relatedImages": [
                    {"image": "quay.io/operator-b:v2.0.1"}
                ]
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("operator-a:stable\n")
            spec_file.write("operator-b:fast\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            # Should select heads from both channels
            self.assertIn("quay.io/operator-a:v1.0.1", images)
            self.assertIn("quay.io/operator-b:v2.0.1", images)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)

    def test_empty_channel_entries(self):
        """Test handling of channel with no entries"""
        objects = [
            {
                "schema": "olm.channel",
                "package": "myoperator",
                "name": "stable",
                "entries": []
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("myoperator:stable\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            # Capture stdout to check warning message
            old_stdout = sys.stdout
            sys.stdout = StringIO()

            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            output = sys.stdout.getvalue()
            sys.stdout = old_stdout

            # Should warn about empty channel
            self.assertIn("Warning", output)
            self.assertIn("no entries", output)
            self.assertEqual(len(images), 0)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)

    def test_single_entry_channel(self):
        """Test channel with single entry (automatically the head)"""
        objects = [
            {
                "schema": "olm.channel",
                "package": "myoperator",
                "name": "stable",
                "entries": [
                    {"name": "myoperator.v1.0.0"}
                ]
            },
            {
                "schema": "olm.bundle",
                "name": "myoperator.v1.0.0",
                "package": "myoperator",
                "relatedImages": [
                    {"image": "quay.io/myoperator:v1.0.0"}
                ]
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("myoperator:stable\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            # Single entry should be selected as head
            self.assertIn("quay.io/myoperator:v1.0.0", images)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)

    def test_malformed_operators_spec(self):
        """Test handling of malformed operator spec entries"""
        objects = [
            {
                "schema": "olm.channel",
                "package": "myoperator",
                "name": "stable",
                "entries": [
                    {"name": "myoperator.v1.0.0"}
                ]
            }
        ]

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as spec_file:
            spec_file.write("myoperator:stable\n")
            spec_file.write("malformed-no-colon\n")
            spec_file.write("too:many:colons:here\n")
            spec_file_path = spec_file.name

        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as img_file:
            img_file_path = img_file.name

        try:
            # Capture stdout to check warning
            old_stdout = sys.stdout
            sys.stdout = StringIO()

            args = self.create_args(spec_file_path, None, img_file_path)
            images = parse_index.extract_images(args, objects)

            output = sys.stdout.getvalue()
            sys.stdout = old_stdout

            # Should warn about malformed records
            self.assertIn("malformed", output)
        finally:
            os.unlink(spec_file_path)
            os.unlink(img_file_path)


class TestLoadRenderedIndex(unittest.TestCase):
    """Test the load_rendered_index function"""

    def test_load_multiple_json_objects(self):
        """Test loading concatenated JSON objects (OLM catalog format)"""
        # Create a file with concatenated JSON objects
        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.json') as f:
            f.write('{"schema": "olm.package", "name": "pkg1"}\n')
            f.write('{"schema": "olm.channel", "name": "stable"}\n')
            f.write('{"schema": "olm.bundle", "name": "bundle1"}\n')
            temp_file = f.name

        try:
            # Mock args
            class MockArgs:
                class MockFile:
                    def __init__(self, path):
                        self.name = path
                        self.mode = 'r'
                rendered_index = MockFile(temp_file)

            # Set global args
            parse_index.args = MockArgs()

            objects = parse_index.load_rendered_index()

            self.assertEqual(len(objects), 3)
            self.assertEqual(objects[0]["schema"], "olm.package")
            self.assertEqual(objects[1]["schema"], "olm.channel")
            self.assertEqual(objects[2]["schema"], "olm.bundle")
        finally:
            os.unlink(temp_file)


if __name__ == '__main__':
    unittest.main()
