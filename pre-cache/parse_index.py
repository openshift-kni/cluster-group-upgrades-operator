import sys
import os
import yaml
import argparse
import traceback
from difflib import SequenceMatcher


def parse_args():
    parser = argparse.ArgumentParser(
        description='Extract list of related images from operator index.')
    parser.add_argument('index_download_path', nargs='?',
                        help="Path where opm index is exportrd")
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


def extract_bundle_names(args):
    bundles = []
    with open(args.operators_spec_file.name, 'r') as p:
        records = [i.split(":") for i in p.read().splitlines() if len(i) > 0]
    for item in records:
        pkg_name = item[0].strip()
        path = os.path.join(args.index_download_path, pkg_name, 'package.yaml')
        with open(path, 'r') as f:
            package = yaml.safe_load(f)
            csv_name = [p.get("currentCSV") for p in
                        package.get("channels")
                        if p.get("name") ==
                        item[1].strip()][0].strip(f"{pkg_name}.")
        with os.scandir(os.path.join(args.index_download_path, pkg_name)) as it:
            bundle_dirs = [entry.name for entry in it if entry.is_dir()]
        bundle_dir = sorted(
            bundle_dirs, key=lambda dir_name: SequenceMatcher(
                None, csv_name, dir_name).ratio())[-1]
        bundles.append(os.path.join(
            args.index_download_path, pkg_name, bundle_dir))
    return bundles


def extract_images(bundles):
    images = []
    try:
        for path in bundles:
            with os.scandir(path) as it:
                csv_file = [entry for entry in it if entry.name.endswith(
                    '.clusterserviceversion.yaml')][-1]
            with open(csv_file.path, 'r') as f:
                csv = yaml.safe_load(f)
            images.extend([im.get('image') for im in csv.get(
                'spec').get('relatedImages')])
    except Exception as e:
        print(e, csv_file)
        traceback.print_exc(file=sys.stdout)
        raise Exception("Failed to extract related images", e)
    finally:
        return images


if __name__ == "__main__":
    try:
        args = parse_args()

        bundles = extract_bundle_names(args)
        images = extract_images(bundles)
        with open(args.img_list_file.name, args.img_list_file.mode) as f:
            f.write('\n'.join(images))
            f.write('\n')
        exit(0)
    except Exception as e:
        print(e)
        traceback.print_exc(file=sys.stdout)
        exit(1)
