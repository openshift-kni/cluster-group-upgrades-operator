#!/usr/bin/python3
import yaml
import sys

file = sys.argv[1]
pull = sys.argv[2]
with open(file,'r') as f:
    obj = yaml.safe_load(f)
for item in obj.get('spec',{}).get('relatedImages',{}):
    if item.get('name','') == "pre-caching-workload":
        item['image'] = pull
    break
with open(file,'w') as f:
    yaml.dump(obj, f, default_flow_style=False)
