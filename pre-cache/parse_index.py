import sys
import json
import argparse
import traceback

def parse_args():
    """ Parse the command line arguments """
    parser = argparse.ArgumentParser(
        description='Extract list of related images from operator index.')
    parser.add_argument('rendered_index', nargs='?',
                        type=argparse.FileType('r'),
                        help="Path where opm index is exported")
    parser.add_argument('operators_spec_file', nargs='?',
                        type=argparse.FileType('r'),
                        help="Path to the list of packages file, \
                            where each line contains \
                            <package>:<channel> record")
    parser.add_argument('img_list_file', nargs='?',
                        type=argparse.FileType('a'),
                        help="Path to the image list file (appended).")

    args = parser.parse_args()
    if len(sys.argv) < 3:
        parser.print_help()
        exit(1)
    return args


def extract_images(args, objects):
    """ Extract related images from the rendered index
    
    Input parameters:
    args: command line arguments, contain operators_spec_file.name
    objects: a list of parsed index objects that comply to OLM schema

    Processing:
    1. Create a structure of packages and channels from the pre-caching
        spec (mounted in the container as args.operators_spec_file.name)
    2. Create a list of bundles for the given packages by taking the latest
        bundle in the channel
    3. For the selected bundles, extract the related images and return them
        as a list

    Returns: list of image pull specifications
    """
    bundles, packages, images = [], [], []
    channels = {}
    # 1. Form the operators packages and channels structure
    with open(args.operators_spec_file.name, 'r') as p:
        # "records" is a list of list: [package, channel] items
        records = [i.split(":") for i in p.read().splitlines() if len(i) > 0]
    
    for item in records:
        if len(item) != 2:
            print(f"operators record {item} is malformed, skipping...")
            continue
        package, channel = [i.strip() for i in item]
        channels[package] = channel
        packages.append(package)
        print(f"will process package {package} channel {channel}")
    # 2. Find the right channels for our packages and get the latest bundle
    for item in objects:
        if item.get("schema") == "olm.channel":
            if item.get("package") in packages and item.get("name") == channels[item.get("package")]:
                latest = item.get("entries")[-1].get("name")
                bundles.append(latest)
    # 3. extract related images from our bundles
    for item in objects:
        if item.get("schema") == "olm.bundle":
            if item.get("name") in bundles and item.get("package") in packages:
                images.extend([elem.get("image") for elem in item.get("relatedImages")])
    return images


def load_rendered_index():
    with open(args.rendered_index.name, args.rendered_index.mode) as f:
        data = f.read().lstrip()
    # Rendered index is not a valid json, but a list
    # of concatenated json blocks. Hence the raw decoder and the loop
    decoder = json.JSONDecoder()
    objects = []
    while data:
        obj, index = decoder.raw_decode(data)
        objects.append(obj)
        data = data[index:].lstrip()
    return objects


if __name__ == "__main__":
    try:
        args = parse_args()
        data = load_rendered_index()
        images = extract_images(args, data)
        with open(args.img_list_file.name, args.img_list_file.mode) as f:
            f.write('\n'.join(images))
            f.write('\n')
        sys.exit(0)
    except Exception as e:
        print(e)
        traceback.print_exc(file=sys.stdout)
        sys.exit(1)
