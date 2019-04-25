import argparse
import os
from os import listdir
from os.path import isfile, join

parser = argparse.ArgumentParser(description='Remove files that match given name.')
parser.add_argument('dir', type=str, help='dir')
parser.add_argument('--file', dest='file', required=False)
parser.add_argument('--files', dest='files', nargs='+', required=False)
parser.add_argument('--dryrun', dest='debug' , required=False)

args = parser.parse_args()
toRemove = []
if args.files:
    toRemove =  args.files[0].split(',')
elif args.file:
    with open(args.file, 'r') as content_file:
        content = content_file.read()
        toRemove = content.split(' ')
else:
    print('Missing required parameter')
    os._exit(1)
debug = args.debug
givenPath = args.dir

files = [f for f in listdir(givenPath) if isfile(join(givenPath, f))]
print('To remove {}'.format(toRemove), )
for fileName in files:
    for remove in toRemove:
        remove = remove.replace(',', '')
        if fileName.find(remove) != -1:
            if not debug:
                os.remove(join(givenPath, fileName))
