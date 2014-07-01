import socket
import fcntl
import struct
import datetime
import time
import os

def internal_ip(pl, ifname):
    if os.path.exists("/tmp/.tmux.internal_ip"):
        readS = ''
        with  open("/tmp/.tmux.internal_ip") as f:
            readS = f.readlines()
        # reader = readS.split("|")
        ip = readS[0]
        report = readS[1]
        ip = ip.rstrip()
        ip += ' '
        report = datetime.datetime.fromtimestamp(float(report)) 
        if ((datetime.datetime.now() - report).seconds/ 60) < 60:
            return ip
    
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    
    ip =  " " + socket.inet_ntoa(fcntl.ioctl(
        s.fileno(),
        0x8915,  # SIOCGIFADDR
        struct.pack('256s', str(ifname[:15]))
    )[20:24])+ " "
    f = open("/tmp/.tmux.internal_ip", 'w')
    string = str(ip) + '\n' + str(time.time())
    f.write(string)
    f.close()
    return ip
