import argparse
import os
import re
from os import listdir
from os.path import isfile, join

parser = argparse.ArgumentParser(description='Process some integers.')
parser.add_argument('--start', dest='start', required=True)
parser.add_argument('--stop', dest='stop', required=True)
parser.add_argument('--prefix', dest='prefix', required=True)
parser.add_argument('--delimiter', dest='delimiter',default='_', required=False)
parser.add_argument('--search', dest='search' , required=False)
parser.add_argument('--replacement', dest='replacement' , required=False)
parser.add_argument('--dryrun', dest='debug' , required=False)

args = parser.parse_args()
print(args)
start = int(args.start)
stop = int(args.stop)
prefix = args.prefix
delimiter = args.delimiter
replacement = args.replacement
search = args.search
givenPath = os.getcwd()

files = [f for f in listdir(givenPath) if isfile(join(givenPath, f))]

for fileName in files:
    if prefix in fileName:
        print('Prefix {} already exists in {} so skip'.format(prefix, fileName))
        continue

    fileIdParts = fileName.split(delimiter)
    if len(fileIdParts) < 2:
        continue
    fileId = delimiter.join(fileIdParts[1:])
    try:
        number = re.search('\d+', fileId)
        if not number:
            print('Unable to search for number {}'.format(fileName))
            continue
        number = int(number.group())
    except ValueError as ex:
        print('unable to process file {} {}'.format(fileName, ex))

    if number < start or number > stop:
        continue


    if search and replacement:
        newName = fileId.replace(search, replacement)
    else:
        newName = '-'.join([prefix, fileId])
    print('{} -> {}'.format(fileName, newName))
    # if not args.debug:
    #     os.rename(fileName, newName)

