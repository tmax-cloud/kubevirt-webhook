FROM ubuntu:18.04
LABEL maintainer "Taesun Lee <taesun_lee@tmax.co.kr>, Joowon Cheong <joowon_cheong@tmax.co.kr>, Haemyung Yang <haemyung_yang@tmax.co.kr>"

ADD controller /controller
ENTRYPOINT ["/controller"]