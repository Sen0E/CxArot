from AcademicReviewOfTeaching import AcademicReviewOfTeaching
import threading


def run(phone, password):
    arot = AcademicReviewOfTeaching(phone, password)
    arot.task()


if __name__ == '__main__':
    users = {"YourPhoneNumber": "YourPassword","YourPhoneNumber2": "YourPassword2"}  # Add more users as needed
    threads = []
    for phone, password in users.items():
        t = threading.Thread(target=run, args=(phone, password))
        threads.append(t)
        t.start()
    for t in threads:
        t.join()
